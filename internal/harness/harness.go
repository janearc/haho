package harness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/discovery"
	"github.com/janearc/haho/internal/filter"
	"github.com/janearc/haho/internal/health"
	"github.com/janearc/haho/internal/model"
	"github.com/janearc/haho/internal/rag"
)

// ErrBusy is returned by Submit when the harness is already processing a
// request. A harness is single-flight by design: the health state machine
// (idle/busy/waiting_*) describes one in-flight job, and operators scale
// throughput by running more harness containers, not by concurrent Submits.
var ErrBusy = errors.New("harness busy: a request is already in flight")

// Harness is the core orchestrator.
type Harness struct {
	cfg            *config.Config
	provider       model.Provider
	inputPipeline  *filter.Pipeline
	outputPipeline *filter.Pipeline
	store          rag.Store // may be nil if SurrealDB unavailable
	cache          rag.Cache // may be nil if cache disabled or SurrealDB unavailable
	health         *health.DefaultTracker
	logger         *slog.Logger
	submitMu       sync.Mutex // enforces single-flight Submit
}

// submitOptions carries per-request overrides for Submit.
type submitOptions struct {
	model string
}

// SubmitOption customizes a single Submit call.
type SubmitOption func(*submitOptions)

// WithModelOverride requests a specific model for this call, overriding the
// configured default. Empty values are ignored.
func WithModelOverride(m string) SubmitOption {
	return func(o *submitOptions) {
		if m != "" {
			o.model = m
		}
	}
}

// Option for dependency injection
type Option func(*Harness)

func WithLogger(l *slog.Logger) Option {
	return func(h *Harness) {
		h.logger = l
	}
}

func WithStore(s rag.Store) Option {
	return func(h *Harness) {
		h.store = s
	}
}

func WithCache(c rag.Cache) Option {
	return func(h *Harness) {
		h.cache = c
	}
}

func WithProvider(p model.Provider) Option {
	return func(h *Harness) {
		h.provider = p
	}
}

func WithHealthTracker(t *health.DefaultTracker) Option {
	return func(h *Harness) {
		h.health = t
	}
}

// New creates a Harness from config.
func New(cfg *config.Config, opts ...Option) (*Harness, error) {
	h := &Harness{
		cfg:            cfg,
		inputPipeline:  filter.NewPipeline(),
		outputPipeline: filter.NewPipeline(),
		health:         health.NewTracker(),
	}

	for _, opt := range opts {
		opt(h)
	}

	if h.logger == nil {
		h.logger = slog.Default()
	}

	if h.provider == nil {
		apiKey := ""
		if cfg.Model.APIKeyEnv != "" {
			apiKey = os.Getenv(cfg.Model.APIKeyEnv)
		}
		// Fail loud rather than sending an empty key to a remote provider,
		// which would surface later as an opaque 401.
		if providerNeedsKey(cfg.Model.Provider) && apiKey == "" {
			return nil, fmt.Errorf("model.provider %q requires an API key but api_key_env=%q resolved to empty", cfg.Model.Provider, cfg.Model.APIKeyEnv)
		}
		p, err := model.NewProvider(cfg.Model.Provider, cfg.Model.Endpoint, cfg.Model.ModelName, apiKey)
		if err != nil {
			return nil, fmt.Errorf("creating provider: %w", err)
		}
		h.provider = p
	}

	h.health.SetModel(fmt.Sprintf("%s/%s", h.provider.Name(), cfg.Model.ModelName))

	if err := h.SetFiltersFromConfig("input", cfg.Filters.Input, true); err != nil {
		return nil, fmt.Errorf("building input filters: %w", err)
	}
	if err := h.SetFiltersFromConfig("output", cfg.Filters.Output, true); err != nil {
		return nil, fmt.Errorf("building output filters: %w", err)
	}

	surrealEndpoint, err := discoverSurrealDB(cfg)
	if err != nil {
		h.logger.Warn("surrealdb discovery failed; store and cache disabled", "error", err)
	} else {
		creds := resolveSurrealCreds(cfg, h.logger)
		if h.store == nil {
			store := rag.NewSurrealStore(surrealEndpoint, cfg.SurrealDB.Namespace, cfg.SurrealDB.Database, creds.user, creds.pass)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if serr := store.EnsureSchema(ctx); serr != nil {
				h.logger.Warn("surrealdb schema bootstrap failed; RAG search disabled until the analyzer and index exist", "error", serr)
			}
			cancel()
			h.store = store
		}
		if h.cache == nil && cfg.Cache.Enabled {
			h.cache = rag.NewSurrealCache(surrealEndpoint, cfg.SurrealDB.Namespace, cfg.SurrealDB.Database, creds.user, creds.pass)
		}
	}

	return h, nil
}

// providerNeedsKey reports whether a provider authenticates with an API key.
func providerNeedsKey(provider string) bool {
	switch provider {
	case "opus", "deepseek":
		return true
	default:
		return false
	}
}

// resolveSurrealCreds reads SurrealDB credentials ("user:pass") from the env var
// named by CredentialsEnv. When unset it falls back to root/root and says so —
// SurrealDB 2.x denies unauthenticated access, so silent-empty creds would only
// surface later as a 403.
func resolveSurrealCreds(cfg *config.Config, logger *slog.Logger) surrealCreds {
	raw := ""
	if cfg.SurrealDB.CredentialsEnv != "" {
		raw = os.Getenv(cfg.SurrealDB.CredentialsEnv)
	}
	if raw == "" {
		logger.Warn("surrealdb credentials env is empty; falling back to default root credentials", "env", cfg.SurrealDB.CredentialsEnv)
	}
	return parseSurrealCreds(raw)
}

// Submit processes a prompt through the full pipeline. It is single-flight:
// a concurrent call returns ErrBusy rather than corrupting the shared health
// state or double-billing the provider.
func (h *Harness) Submit(ctx context.Context, messages []filter.Message, opts ...SubmitOption) (*model.Response, error) {
	if !h.submitMu.TryLock() {
		return nil, ErrBusy
	}
	defer h.submitMu.Unlock()

	var so submitOptions
	for _, opt := range opts {
		opt(&so)
	}
	modelName := h.cfg.Model.ModelName
	if so.model != "" {
		modelName = so.model
	}

	defer h.health.SetState(health.StateIdle, "")
	h.health.SetState(health.StateBusy, "processing input")

	// Run input filters on each message. Process clones internally, so the
	// caller's slice is never mutated.
	var filteredMessages []filter.Message
	for _, msg := range messages {
		resMsg, err := h.inputPipeline.Process(ctx, &msg)
		if err != nil {
			return nil, fmt.Errorf("input filter error: %w", err)
		}
		filteredMessages = append(filteredMessages, *resMsg)
	}

	// Check cache. The key is computed pre-RAG so it is deterministic; the
	// retrieved RAG context is intentionally not part of the key.
	cacheKey := rag.CacheKey(filteredMessages, modelName)
	if h.cache != nil {
		cachedResp, hit, err := h.cache.Get(ctx, cacheKey)
		if err != nil {
			h.logger.Warn("cache get failed", "error", err)
		} else if hit {
			cachedResp.Cached = true
			
			// Run output filter on cached response
			outMsg := &filter.Message{Role: "assistant", Content: cachedResp.Content}
			finalMsg, err := h.outputPipeline.Process(ctx, outMsg)
			if err != nil {
				return nil, fmt.Errorf("output filter error on cached resp: %w", err)
			}
			cachedResp.Content = finalMsg.Content
			return cachedResp, nil
		}
	}

	// 4. RAG augmentation
	if h.store != nil && len(filteredMessages) > 0 {
		lastMsg := filteredMessages[len(filteredMessages)-1]
		results, err := h.store.Search(ctx, lastMsg.Content, 3)
		if err != nil {
			h.logger.Warn("RAG search failed", "error", err)
		} else if len(results) > 0 {
			var contextBuilder strings.Builder
			contextBuilder.WriteString("Context:\n")
			for _, r := range results {
				contextBuilder.WriteString(r.Document.Content)
				contextBuilder.WriteString("\n---\n")
			}
			sysMsg := filter.Message{
				Role:    "system",
				Content: contextBuilder.String(),
			}
			filteredMessages = append([]filter.Message{sysMsg}, filteredMessages...)
		}
	}

	h.health.SetState(health.StateWaitingSubprocess, "waiting on model provider")
	req := &model.Request{
		Messages: filteredMessages,
		Model:    modelName,
	}
	resp, err := h.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider generate: %w", err)
	}

	h.health.SetState(health.StateBusy, "processing output")

	if h.cache != nil {
		ttl := time.Duration(h.cfg.Cache.TTLSeconds) * time.Second
		if err := h.cache.Set(ctx, cacheKey, resp, ttl); err != nil {
			h.logger.Warn("cache set failed", "error", err)
		}
	}

	outMsg := &filter.Message{Role: "assistant", Content: resp.Content}
	finalMsg, err := h.outputPipeline.Process(ctx, outMsg)
	if err != nil {
		return nil, fmt.Errorf("output filter error: %w", err)
	}
	resp.Content = finalMsg.Content

	return resp, nil
}

func (h *Harness) Health() *health.Status {
	return h.health.GetStatus()
}

func (h *Harness) Config() *config.Config {
	return h.cfg
}

func (h *Harness) SetInputFilters(filters []filter.Filter) {
	h.inputPipeline.SetFilters(filters)
}

func (h *Harness) SetOutputFilters(filters []filter.Filter) {
	h.outputPipeline.SetFilters(filters)
}

func (h *Harness) Close() error {
	if h.store != nil {
		h.store.Close()
	}
	if h.provider != nil {
		return h.provider.Close()
	}
	return nil
}

// BuildFilters builds a filter pipeline from trusted config entries (e.g. the
// on-disk config file). Use SetFiltersFromConfig with trusted=false for entries
// that arrive over the control API.
func BuildFilters(entries []config.FilterEntry) ([]filter.Filter, error) {
	return buildFilters(entries, true)
}

func buildFilters(entries []config.FilterEntry, trusted bool) ([]filter.Filter, error) {
	var filters []filter.Filter
	for _, entry := range entries {
		f, err := newFilter(entry, trusted)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, nil
}

// SetFiltersFromConfig replaces a stage's pipeline. trusted must be false for
// entries received over the network so that untrusted callers cannot choose
// which binary the RTK filter executes.
func (h *Harness) SetFiltersFromConfig(stage string, entries []config.FilterEntry, trusted bool) error {
	filters, err := buildFilters(entries, trusted)
	if err != nil {
		return err
	}
	if stage == "input" {
		h.SetInputFilters(filters)
	} else if stage == "output" {
		h.SetOutputFilters(filters)
	} else {
		return fmt.Errorf("unknown stage: %q", stage)
	}
	return nil
}

func newFilter(entry config.FilterEntry, trusted bool) (filter.Filter, error) {
	switch strings.ToUpper(entry.Type) {
	case "RTK":
		// binary_path chooses which executable runs with message content on
		// stdin. Honoring it from an untrusted caller is arbitrary command
		// execution, so it is accepted only from trusted (on-disk) config.
		binaryPath := ""
		if trusted {
			binaryPath = entry.Params["binary_path"]
		} else if entry.Params["binary_path"] != "" {
			return nil, fmt.Errorf("rtk filter: binary_path may not be set via the control API")
		}
		return filter.NewRTKFilter(binaryPath)
	case "PASSTHROUGH":
		return filter.NewPassthroughFilter(), nil
	case "LOGGER":
		return filter.NewLoggerFilter(nil), nil
	default:
		return nil, fmt.Errorf("unknown filter type: %q", entry.Type)
	}
}

type surrealCreds struct{ user, pass string }

func parseSurrealCreds(env string) surrealCreds {
	parts := strings.Split(env, ":")
	if len(parts) == 2 {
		return surrealCreds{user: parts[0], pass: parts[1]}
	}
	return surrealCreds{user: "root", pass: "root"}
}

func discoverSurrealDB(cfg *config.Config) (string, error) {
	disc, err := discovery.New(cfg.SurrealDB.Discovery, cfg.SurrealDB.Host, cfg.SurrealDB.Port)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoint, err := disc.Discover(ctx, cfg.SurrealDB.ServiceName, cfg.SurrealDB.Namespace)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s", endpoint.Addr()), nil
}
