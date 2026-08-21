package backend

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/janearc/haho/gen/commonpb"
	"github.com/janearc/haho/gen/jobpb"
)

// script writes an executable shell script and returns its path, so a test can
// stand in for the CLI without one being installed.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return p
}

// useCLI points hahod's configured executable at a stub for one test. This is
// the ONLY seam that names the executable -- a spec cannot -- so it is also the
// test seam.
func useCLI(t *testing.T, body string) {
	t.Helper()
	prev := cliCommand
	ConfigureClaudeCLI(script(t, body))
	t.Cleanup(func() { cliCommand = prev })
}

func cliSpec(params map[string]string) *jobpb.JobSpec {
	return &jobpb.JobSpec{
		JobId:   "test-cli-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "rule on this"}},
		Backend: &jobpb.Backend{Kind: jobpb.Backend_CLAUDE_CLI, Model: "claude-fable-5", Params: params},
	}
}

func TestClaudeCLIRefusesTokenEnv(t *testing.T) {
	// The point of this kind is that the credential is the CLI's. A token_env
	// here would name a secret nothing reads -- configured-looking and inert.
	spec := cliSpec(nil)
	spec.Backend.TokenEnv = "HOLDEN_ANTHROPIC_KEY"
	if _, err := newClaudeCLI(spec); err == nil {
		t.Fatal("newClaudeCLI accepted a token_env; it must refuse one")
	}
}

func TestClaudeCLIRefusesBaseURL(t *testing.T) {
	spec := cliSpec(nil)
	spec.Backend.BaseUrl = "https://api.anthropic.com"
	if _, err := newClaudeCLI(spec); err == nil {
		t.Fatal("newClaudeCLI accepted a base_url; this kind does not speak HTTP")
	}
}

func TestClaudeCLIRefusesRequestorNamedExecutable(t *testing.T) {
	// THE SECURITY PROPERTY. A same-uid requestor naming the executable would
	// have hahod run anything it liked, and (before childEnv) with hahod's own
	// environment. Refused, not ignored: a param that silently does nothing is
	// the same "reads as configured and is not" fault this kind refuses
	// token_env for.
	if _, err := newClaudeCLI(cliSpec(map[string]string{"command": "/bin/sh"})); err == nil {
		t.Fatal("a spec named the executable and was accepted")
	}
	if _, err := newClaudeCLI(cliSpec(map[string]string{"args_prefix": "--dangerously"})); err == nil {
		t.Fatal("a spec supplied argv and was accepted")
	}
}

func TestClaudeCLIUsesHahodsConfiguredExecutable(t *testing.T) {
	useCLI(t, "cat")
	b, err := newClaudeCLI(cliSpec(nil))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	if got := b.(*claudecli).command; got != cliCommand {
		t.Errorf("command = %q, want hahod's configured %q", got, cliCommand)
	}
}

func TestClaudeCLIDefaults(t *testing.T) {
	b, err := newClaudeCLI(cliSpec(nil))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	if c := b.(*claudecli); c.timeout.Seconds() != 900 {
		t.Errorf("timeout = %v, want 900s", c.timeout)
	}
}

func TestClaudeCLITimeoutCeiling(t *testing.T) {
	if _, err := newClaudeCLI(cliSpec(map[string]string{"timeout_s": "99999"})); err == nil {
		t.Fatal("accepted a timeout above the ceiling; one job could park a lane slot indefinitely")
	}
}

func TestClaudeCLIReadsPromptOnStdin(t *testing.T) {
	// argv is world-readable in ps and has a length ceiling a ruling bundle
	// would breach.
	useCLI(t, "cat")
	b, _ := newClaudeCLI(cliSpec(nil))
	res, err := b.Run(context.Background(), cliSpec(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "rule on this" {
		t.Errorf("Text = %q, want the prompt echoed from stdin", res.Text)
	}
	if res.TokensIn != -1 || res.TokensOut != -1 {
		t.Errorf("tokens = %d/%d, want -1/-1 (absent, not zero)", res.TokensIn, res.TokensOut)
	}
}

func TestClaudeCLIEmptyStdoutIsAnError(t *testing.T) {
	// WHOLE_OR_ERROR: a non-answer must not land as one.
	useCLI(t, "exit 0")
	b, _ := newClaudeCLI(cliSpec(nil))
	if _, err := b.Run(context.Background(), cliSpec(nil)); err == nil {
		t.Fatal("Run landed an empty reply as success")
	}
}

func TestClaudeCLINonZeroExitCarriesStderr(t *testing.T) {
	useCLI(t, "echo 'not logged in' >&2; exit 3")
	b, _ := newClaudeCLI(cliSpec(nil))
	_, err := b.Run(context.Background(), cliSpec(nil))
	if err == nil {
		t.Fatal("Run reported success on a non-zero exit")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error %q does not carry stderr; the diagnosis is the whole value", err)
	}
}

func TestClaudeCLITimeoutSaysSo(t *testing.T) {
	useCLI(t, "sleep 5")
	b, _ := newClaudeCLI(cliSpec(map[string]string{"timeout_s": "1"}))
	_, err := b.Run(context.Background(), cliSpec(nil))
	if err == nil {
		t.Fatal("Run reported success past its timeout")
	}
	// A killed subprocess otherwise reads as a crash rather than a bound doing
	// its job.
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error %q does not name the timeout", err)
	}
}

func TestClaudeCLIRunsWithoutTools(t *testing.T) {
	// A payload is requestor-supplied text; for a ruling it carries design docs
	// from public repositories. A tool-bearing CLI can be steered by that text
	// into acting as the operator on this host. If this fails, an untrusted
	// payload gained a way to act.
	useCLI(t, `printf '%s\n' "$@"`)
	b, _ := newClaudeCLI(cliSpec(nil))
	res, err := b.Run(context.Background(), cliSpec(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(res.Text, "\n")
	idx := -1
	for i, l := range lines {
		if l == "--tools" {
			idx = i
			break
		}
	}
	if idx == -1 {
		t.Fatalf("--tools absent from argv: %q", res.Text)
	}
	if idx+1 >= len(lines) || lines[idx+1] != "" {
		t.Errorf("--tools was not followed by an empty value: %q", res.Text)
	}
}

func TestClaudeCLIChildDoesNotInheritHahodsSecrets(t *testing.T) {
	// THE OTHER HALF OF THE SECURITY PROPERTY, and the one the RFC states
	// outright: "the environment is constructed from the spec's references and
	// nothing else is inherited" (section 14). hahod resolves
	// HOLDEN_ANTHROPIC_KEY from 1Password into its OWN environment at start;
	// handing that to a requestor-triggered subprocess would be an exfiltration
	// path, and an earlier draft of this file did exactly that.
	t.Setenv("HOLDEN_ANTHROPIC_KEY", "sentinel-must-not-appear")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "sentinel-must-not-appear")
	useCLI(t, `printf '%s' "${HOLDEN_ANTHROPIC_KEY:-ABSENT}/${AWS_SECRET_ACCESS_KEY:-ABSENT}"`)
	b, _ := newClaudeCLI(cliSpec(nil))
	res, err := b.Run(context.Background(), cliSpec(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(res.Text, "sentinel") {
		t.Fatalf("the child inherited a secret from hahod's environment: %q", res.Text)
	}
	if res.Text != "ABSENT/ABSENT" {
		t.Errorf("child env = %q, want both absent", res.Text)
	}
}

func TestClaudeCLIChildKeepsWhatTheCLINeeds(t *testing.T) {
	// Short and boring on purpose: a home to find its own credentials, a PATH
	// to find its runtime. Dropping these would break the CLI rather than
	// secure it.
	useCLI(t, `printf '%s' "${HOME:-NOHOME}|${PATH:+HASPATH}"`)
	b, _ := newClaudeCLI(cliSpec(nil))
	res, err := b.Run(context.Background(), cliSpec(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.HasPrefix(res.Text, "NOHOME") || !strings.Contains(res.Text, "HASPATH") {
		t.Errorf("child env dropped HOME or PATH: %q", res.Text)
	}
}

func TestClaudeCLICarriesOtelAttribution(t *testing.T) {
	// Without this the hahod path silently drops the per-repo, per-PR spend
	// attribution the shim already delivers.
	useCLI(t, `printf '%s' "$OTEL_RESOURCE_ATTRIBUTES"`)
	b, _ := newClaudeCLI(cliSpec(map[string]string{
		"otel_resource_attributes": "mesh.component=holden-judge,mesh.repo=kestrel,mesh.pr=2",
	}))
	res, err := b.Run(context.Background(), cliSpec(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "mesh.repo=kestrel") || !strings.Contains(res.Text, "mesh.pr=2") {
		t.Errorf("child OTEL_RESOURCE_ATTRIBUTES = %q, want the spec's attribution", res.Text)
	}
}

func TestClaudeCLIAppendsRatherThanOverwritingAttribution(t *testing.T) {
	// hahod may be attributing its own work through this variable.
	t.Setenv(otelResourceAttrs, "deployment.environment=fleet")
	useCLI(t, `printf '%s' "$OTEL_RESOURCE_ATTRIBUTES"`)
	b, _ := newClaudeCLI(cliSpec(map[string]string{"otel_resource_attributes": "mesh.repo=holden"}))
	res, err := b.Run(context.Background(), cliSpec(nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "deployment.environment=fleet") {
		t.Errorf("inherited attribution was overwritten: %q", res.Text)
	}
	if !strings.Contains(res.Text, "mesh.repo=holden") {
		t.Errorf("spec attribution missing: %q", res.Text)
	}
}

func TestClaudeCLIPromptLabelsNonUserRoles(t *testing.T) {
	spec := &jobpb.JobSpec{
		JobId: "test-cli-3",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Messages{
			Messages: &jobpb.Messages{Messages: []*commonpb.ChatMessage{
				{Role: "system", Content: "be exact"},
				{Role: "user", Content: "the diff"},
			}},
		}},
		Backend: &jobpb.Backend{Kind: jobpb.Backend_CLAUDE_CLI},
	}
	got, err := cliPrompt(spec)
	if err != nil {
		t.Fatalf("cliPrompt: %v", err)
	}
	// -p takes one prompt; silently dropping a role the requestor set would
	// change the job without saying so.
	if !strings.Contains(got, "SYSTEM:") || !strings.Contains(got, "be exact") {
		t.Errorf("prompt %q dropped or unlabelled the system turn", got)
	}
	if !strings.Contains(got, "the diff") {
		t.Errorf("prompt %q dropped the user turn", got)
	}
}

func TestClaudeCLIEmptyPayloadRefused(t *testing.T) {
	spec := &jobpb.JobSpec{
		JobId:   "test-cli-4",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "   "}},
		Backend: &jobpb.Backend{Kind: jobpb.Backend_CLAUDE_CLI},
	}
	if _, err := cliPrompt(spec); err == nil {
		t.Fatal("cliPrompt accepted an empty prompt")
	}
}
