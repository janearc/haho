package harness

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/janearc/haho/internal/config"
	"github.com/janearc/haho/internal/discovery"
	"github.com/janearc/haho/internal/filter"
	"github.com/janearc/haho/internal/health"
	"github.com/janearc/haho/internal/model"
	"github.com/janearc/haho/internal/rag"
)

// Harness is the core orchestrator.
type Harness struct {
	cfg            *config.Config
	provider       model.Provider
	inputPipeline  *filter.Pipeline
	outputPipeline *filter.Pipeline
	store          rag.Store // may be nil if SurrealDB unavailable
	cache          rag.Cache // may be nil if cache disabled or SurrealDB unavailable
	health         *health.DefaultTracker
	logger         *log.Logger
}

// Option for dependency injection
type Option func(*Harness)

func WithLogger(l *log.Logger) Option {
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
		h.logger = log.Default()
	}

	if h.provider == nil {
		p, err := model.NewProvider(cfg.Model.Provider, cfg.Model.Endpoint, cfg.Model.ModelName, "") // api key not fetched from env in this PoC stub
		if err != nil {
			return nil, fmt.Errorf("creating provider: %w", err)
		}
		h.provider = p
	}

	h.health.SetModel(fmt.Sprintf("%s/%s", h.provider.Name(), cfg.Model.ModelName))

	if err := h.SetFiltersFromConfig("input", cfg.Filters.Input); err != nil {
		return nil, fmt.Errorf("building input filters: %w", err)
	}
	if err := h.SetFiltersFromConfig("output", cfg.Filters.Output); err != nil {
		return nil, fmt.Errorf("building output filters: %w", err)
	}

	surrealEndpoint, err := discoverSurrealDB(cfg)
	if err != nil {
		h.logger.Printf("warning: discovering surrealdb: %v (store and cache will be disabled)", err)
	} else {
		creds := parseSurrealCreds("") // empty in this stub
		if h.store == nil {
			h.store = rag.NewSurrealStore(surrealEndpoint, cfg.SurrealDB.Namespace, cfg.SurrealDB.Database, creds.user, creds.pass)
		}
		if h.cache == nil && cfg.Cache.Enabled {
			h.cache = rag.NewSurrealCache(surrealEndpoint, cfg.SurrealDB.Namespace, cfg.SurrealDB.Database, creds.user, creds.pass)
		}
	}

	return h, nil
}

// Submit processes a prompt through the full pipeline.
func (h *Harness) Submit(ctx context.Context, messages []filter.Message) (*model.Response, error) {
	defer h.health.SetState(health.StateIdle, "")
	h.health.SetState(health.StateBusy, "processing input")

	// 2. Run input filters on each message
	var filteredMessages []filter.Message
	for _, msg := range messages {
		// make a copy for pointer passing
		msgPtr := &msg
		resMsg, err := h.inputPipeline.Process(ctx, msgPtr)
		if err != nil {
			return nil, fmt.Errorf("input filter error: %w", err)
		}
		filteredMessages = append(filteredMessages, *resMsg)
	}

	// 3. Check cache
	cacheKey := rag.CacheKey(filteredMessages, h.cfg.Model.ModelName)
	if h.cache != nil {
		cachedResp, hit, err := h.cache.Get(ctx, cacheKey)
		if err != nil {
			h.logger.Printf("warning: cache get error: %v", err)
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
			h.logger.Printf("warning: RAG search error: %v", err)
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
		Model:    h.cfg.Model.ModelName,
	}
	resp, err := h.provider.Generate(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("provider generate: %w", err)
	}

	h.health.SetState(health.StateBusy, "processing output")

	if h.cache != nil {
		if err := h.cache.Set(ctx, cacheKey, resp, 0); err != nil { // 0 for now
			h.logger.Printf("warning: cache set error: %v", err)
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

// BuildFilters is exported for server config reload.
func BuildFilters(entries []config.FilterEntry) ([]filter.Filter, error) {
	var filters []filter.Filter
	for _, entry := range entries {
		f, err := newFilter(entry)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, nil
}

func (h *Harness) SetFiltersFromConfig(stage string, entries []config.FilterEntry) error {
	filters, err := BuildFilters(entries)
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

func newFilter(entry config.FilterEntry) (filter.Filter, error) {
	switch strings.ToUpper(entry.Type) {
	case "RTK":
		return filter.NewRTKFilter(entry.Params["binary_path"])
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
	endpoint, err := disc.Discover(context.Background(), cfg.SurrealDB.ServiceName, cfg.SurrealDB.Namespace)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%s", endpoint.Addr()), nil
}
