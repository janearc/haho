package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/janearc/haho/gen/jobpb"
)

// claudecli runs Backend.Kind CLAUDE_CLI: an agent CLI spawned as a
// subprocess, which carries its OWN authentication. This is the seam holden's
// shim already used -- `claude -p`, prompt on stdin, reply on stdout -- moved
// behind the job contract so a ruling becomes an admitted, accounted job
// instead of a bare fork.
//
// WHY THIS IS NOT ANTHROPIC_COMPATIBLE. That kind reaches the service over
// HTTP with an api key named by token_env, and billing follows the key. This
// one spends whatever the CLI is logged in as, and there is no key anywhere
// in the spec. Same upstream service, different payer -- so it is a different
// kind rather than a param, because a kind/lane/credential mismatch should be
// impossible to express rather than merely discouraged.
//
// params understood by this backend:
//
//	command     — the executable, default "claude"
//	args_prefix — space-separated args placed BEFORE -p (e.g. a --settings
//	              flag); empty by default
//	timeout_s   — whole-attempt timeout, default 900. A ruling runs minutes,
//	              not seconds, so the default is well above the api kinds'.
//	otel_resource_attributes — comma-separated key=value pairs set on the
//	              child's OTEL_RESOURCE_ATTRIBUTES, which the CLI turns into
//	              resource attributes and the collector into labels.
//
// WHY THAT LAST ONE EXISTS. The shim this replaces set that variable on the
// subprocess itself, so "what did this ruling cost" was answerable per repo
// and per pull request. A requestor talking to hahod cannot reach hahod's
// child, so without a way to carry the attribution through the spec, moving
// off the shim would silently DROP accounting that works today. Carrying it
// is not a feature; it is the cost of not regressing.
type claudecli struct {
	command string
	model   string
	prefix  []string
	timeout time.Duration
	otelRes string
}

// cliMaxOutputBytes caps what a runaway subprocess can stuff into a
// completion record. Same posture as the api kinds' response cap.
const cliMaxOutputBytes = 1 << 20

// otelResourceAttrs is the OpenTelemetry variable the CLI reads to label the
// telemetry it exports.
const otelResourceAttrs = "OTEL_RESOURCE_ATTRIBUTES"

func init() {
	Register(jobpb.Backend_CLAUDE_CLI, newClaudeCLI)
}

func newClaudeCLI(spec *jobpb.JobSpec) (Backend, error) {
	b := spec.GetBackend()

	// The credential is the CLI's own. A token_env here would name a secret
	// that nothing reads, which reads as configured and is not -- exactly the
	// shape of fault this estate keeps paying for. Refuse it at construction.
	if envName := b.GetTokenEnv(); envName != "" {
		return nil, fmt.Errorf("claudecli: descriptor carries token_env %q, but this kind authenticates as the CLI itself; leave token_env empty", envName)
	}
	// base_url likewise: the CLI decides where it goes.
	if b.GetBaseUrl() != "" {
		return nil, fmt.Errorf("claudecli: descriptor carries base_url %q, but this kind does not speak HTTP; leave base_url empty", b.GetBaseUrl())
	}

	command := "claude"
	if raw, ok := b.GetParams()["command"]; ok {
		if strings.TrimSpace(raw) == "" {
			return nil, fmt.Errorf("claudecli: params.command is empty")
		}
		command = raw
	}

	var prefix []string
	if raw, ok := b.GetParams()["args_prefix"]; ok {
		prefix = strings.Fields(raw)
	}

	timeout := 900 * time.Second
	if raw, ok := b.GetParams()["timeout_s"]; ok {
		n, err := positiveInt(raw)
		if err != nil {
			return nil, fmt.Errorf("claudecli: params.timeout_s %q is %w", raw, err)
		}
		if n > maxTimeoutSeconds {
			// A lane slot is held for the whole attempt.
			return nil, fmt.Errorf("claudecli: params.timeout_s %d exceeds the %ds ceiling", n, maxTimeoutSeconds)
		}
		timeout = time.Duration(n) * time.Second
	}

	return &claudecli{
		command: command,
		model:   b.GetModel(),
		prefix:  prefix,
		timeout: timeout,
		otelRes: b.GetParams()["otel_resource_attributes"],
	}, nil
}

// prompt renders the payload as the single string the CLI reads on stdin.
// A system message is prepended and labelled rather than dropped: the CLI's
// -p takes one prompt, and silently discarding a role the requestor set
// would change the job without saying so.
func cliPrompt(spec *jobpb.JobSpec) (string, error) {
	switch body := spec.GetPayload().GetBody().(type) {
	case *jobpb.Payload_Text:
		if strings.TrimSpace(body.Text) == "" {
			return "", fmt.Errorf("claudecli: payload text is empty")
		}
		return body.Text, nil
	case *jobpb.Payload_Messages:
		var b strings.Builder
		for _, m := range body.Messages.GetMessages() {
			if strings.TrimSpace(m.GetContent()) == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			switch m.GetRole() {
			case "user":
				b.WriteString(m.GetContent())
			default:
				// role-labelled so a system or assistant turn is visibly
				// what it is, rather than merged into the user's words.
				b.WriteString(strings.ToUpper(m.GetRole()))
				b.WriteString(":\n")
				b.WriteString(m.GetContent())
			}
		}
		if b.Len() == 0 {
			return "", fmt.Errorf("claudecli: payload carries no messages")
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("claudecli: unservable payload shape") // admission refuses this first
	}
}

func (c *claudecli) Run(ctx context.Context, spec *jobpb.JobSpec) (*Result, error) {
	prompt, err := cliPrompt(spec)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	args := append([]string{}, c.prefix...)
	args = append(args, "-p")
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	cmd := exec.CommandContext(ctx, c.command, args...)
	// APPENDED to whatever hahod already carries, never assigned. hahod may
	// be attributing its own work through this variable; overwriting it would
	// silently drop that, and the symptom would be a gap in someone else's
	// accounting rather than an error here.
	if c.otelRes != "" {
		merged := c.otelRes
		if inherited := os.Getenv(otelResourceAttrs); inherited != "" {
			merged = inherited + "," + c.otelRes
		}
		cmd.Env = append(os.Environ(), otelResourceAttrs+"="+merged)
	}
	// The prompt goes in on STDIN, never in argv: argv is world-readable in
	// ps and has a length ceiling a ruling bundle would breach.
	cmd.Stdin = strings.NewReader(prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		// Name the real fact. A killed subprocess otherwise surfaces as a
		// bare "signal: killed" that reads like a crash.
		return nil, fmt.Errorf("claudecli: %s exceeded the %s attempt timeout", c.command, c.timeout)
	}
	if runErr != nil {
		return nil, fmt.Errorf("claudecli: %s: %w: %s", c.command, runErr, truncate(strings.TrimSpace(stderr.String()), 512))
	}
	if stdout.Len() >= cliMaxOutputBytes {
		return nil, fmt.Errorf("claudecli: output reached the %d-byte cap; refusing to land a possibly-truncated reply", cliMaxOutputBytes)
	}

	text := strings.TrimSpace(stdout.String())
	if text == "" {
		// Exit 0 with nothing on stdout is not a whole answer, and
		// WHOLE_OR_ERROR means it must not land as one.
		return nil, fmt.Errorf("claudecli: %s exited 0 with empty stdout: %s", c.command, truncate(strings.TrimSpace(stderr.String()), 512))
	}

	model := c.model
	if model == "" {
		// The CLI chose; we do not know which, and guessing would put a
		// fabricated model name in the ledger.
		model = "claude-cli/unspecified"
	}
	// The CLI reports no token counts on this path. Absent is not zero.
	return &Result{Text: text, ModelUsed: model, TokensIn: -1, TokensOut: -1}, nil
}
