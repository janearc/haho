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
// THE EXECUTABLE IS NOT THE REQUESTOR'S TO NAME. It is resolved from hahod's
// own configuration (-claude-cli / HAHO_CLAUDE_CLI), the same pattern
// HAHOD_ADDR and HAHOD_STORE already use. An earlier draft took it from the
// spec's params, and that was wrong in a way worth recording: a same-uid
// requestor could name ANY executable and hahod would run it -- with hahod's
// process environment, which since the 1Password change carries a resolved
// secret. Nothing about landing this kind required it, and a spec naming
// `command` or `args_prefix` is now REFUSED rather than ignored.
//
// params understood by this backend:
//
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
	timeout time.Duration
	otelRes string
}

// command is hahod's, set once at startup from its own flags/env. Not a
// package default a spec can reach.
var cliCommand = "claude"

// ConfigureClaudeCLI names the agent CLI hahod will exec for CLAUDE_CLI jobs.
// Called from cmd/hahod at startup, before anything is admitted.
func ConfigureClaudeCLI(path string) {
	if path != "" {
		cliCommand = path
	}
}

// childEnv builds the subprocess environment EXPLICITLY rather than handing it
// hahod's own. RFC section 14: "the environment is constructed from the spec's
// references and nothing else is inherited." Inheriting would hand a
// requestor-triggered process every secret hahod holds -- including the
// 1Password-resolved HOLDEN_ANTHROPIC_KEY, which belongs to the HTTP kinds and
// has no business here.
//
// The passthrough list is deliberately short and boring: the CLI needs a home
// to find its own credentials and configuration, a PATH to find its runtime,
// locale so its output is not mangled, and USER/LOGNAME because the CLI
// resolves the credential it owns by ACCOUNT NAME -- without USER it answers
// "Not logged in" and exits 1 with nothing on stderr, which is how the
// 2026-08-22 rehearsal landed two FAILED jobs in a second each (issue 13).
// Identity is not a secret; the credential it unlocks never passes through
// here. Everything else is dropped, and anything OTEL_ is carried so
// telemetry keeps working.
func childEnv(otelRes string) []string {
	const otelPrefix = "OTEL_"
	keep := map[string]bool{
		"HOME": true, "PATH": true, "LANG": true, "LC_ALL": true, "TZ": true,
		"USER": true, "LOGNAME": true,
	}
	out := make([]string, 0, 8)
	inheritedOtel := ""
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k := kv[:eq]
		if keep[k] {
			out = append(out, kv)
			continue
		}
		if strings.HasPrefix(k, otelPrefix) {
			if k == otelResourceAttrs {
				inheritedOtel = kv[eq+1:]
				continue // merged below, never doubled
			}
			out = append(out, kv)
		}
	}
	// APPENDED to whatever hahod already attributes, never assigned: hahod may
	// be attributing its own work through this variable and overwriting it
	// would drop that silently.
	merged := otelRes
	if inheritedOtel != "" && merged != "" {
		merged = inheritedOtel + "," + merged
	} else if merged == "" {
		merged = inheritedOtel
	}
	if merged != "" {
		out = append(out, otelResourceAttrs+"="+merged)
	}
	return out
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

	// A spec must not choose what hahod executes, nor prepend argv to it.
	// Refused rather than ignored: a param that silently does nothing is the
	// same "reads as configured and is not" fault this kind refuses token_env
	// for.
	if _, ok := b.GetParams()["command"]; ok {
		return nil, fmt.Errorf("claudecli: params.command is not accepted; the executable is hahod's own configuration (-claude-cli / HAHO_CLAUDE_CLI), never the requestor's")
	}
	if _, ok := b.GetParams()["args_prefix"]; ok {
		return nil, fmt.Errorf("claudecli: params.args_prefix is not accepted; a requestor does not supply argv to a process hahod runs")
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
		command: cliCommand,
		model:   b.GetModel(),
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

	args := []string{"-p"}
	// NO TOOLS, and this is load-bearing rather than tidy. A payload is text
	// the requestor supplied, and for a ruling it carries design docs from
	// public repositories -- untrusted text by construction. A tool-bearing
	// CLI can be steered by that text into acting as the operator on this
	// host, which is a different and much worse failure than a bad answer.
	// The seam's contract is prompt-to-text; tools are not part of it.
	//
	// Passed here rather than left to params.args_prefix on purpose: this
	// must not be forgettable, and args_prefix is split on spaces and so
	// cannot express an empty-string argument at all.
	args = append(args, "--tools", "")
	if c.model != "" {
		args = append(args, "--model", c.model)
	}

	cmd := exec.CommandContext(ctx, c.command, args...)
	// ALWAYS set, never left nil: a nil Env means "inherit everything", which
	// is exactly what section 14 forbids and what would hand this child
	// hahod's secrets.
	cmd.Env = childEnv(c.otelRes)
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
