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

// script writes an executable shell script and returns its path, so a test
// can stand in for the CLI without one being installed.
func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return p
}

func cliSpec(command string, params map[string]string) *jobpb.JobSpec {
	if params == nil {
		params = map[string]string{}
	}
	params["command"] = command
	return &jobpb.JobSpec{
		JobId:   "test-cli-1",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "rule on this"}},
		Backend: &jobpb.Backend{Kind: jobpb.Backend_CLAUDE_CLI, Params: params},
	}
}

func TestClaudeCLIRefusesTokenEnv(t *testing.T) {
	// The whole point of this kind is that the credential is the CLI's. A
	// token_env here would name a secret nothing reads — configured-looking
	// and inert, which is the fault shape this estate keeps paying for.
	spec := cliSpec("/bin/true", nil)
	spec.Backend.TokenEnv = "HOLDEN_ANTHROPIC_KEY"
	if _, err := newClaudeCLI(spec); err == nil {
		t.Fatal("newClaudeCLI accepted a token_env; it must refuse one")
	}
}

func TestClaudeCLIRefusesBaseURL(t *testing.T) {
	spec := cliSpec("/bin/true", nil)
	spec.Backend.BaseUrl = "https://api.anthropic.com"
	if _, err := newClaudeCLI(spec); err == nil {
		t.Fatal("newClaudeCLI accepted a base_url; this kind does not speak HTTP")
	}
}

func TestClaudeCLIDefaults(t *testing.T) {
	b, err := newClaudeCLI(&jobpb.JobSpec{
		JobId:   "test-cli-2",
		Payload: &jobpb.Payload{Body: &jobpb.Payload_Text{Text: "x"}},
		Backend: &jobpb.Backend{Kind: jobpb.Backend_CLAUDE_CLI},
	})
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	c := b.(*claudecli)
	if c.command != "claude" {
		t.Errorf("command = %q, want claude", c.command)
	}
	if c.timeout.Seconds() != 900 {
		t.Errorf("timeout = %v, want 900s", c.timeout)
	}
}

func TestClaudeCLITimeoutCeiling(t *testing.T) {
	if _, err := newClaudeCLI(cliSpec("/bin/true", map[string]string{"timeout_s": "99999"})); err == nil {
		t.Fatal("accepted a timeout above the ceiling; one job could park a lane slot indefinitely")
	}
}

func TestClaudeCLIReadsPromptOnStdin(t *testing.T) {
	// The prompt must arrive on stdin, never argv: argv is world-readable
	// in ps and has a length ceiling a ruling bundle would breach.
	b, err := newClaudeCLI(cliSpec(script(t, "cat"), nil))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	res, err := b.Run(context.Background(), cliSpec("unused", nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Text != "rule on this" {
		t.Errorf("Text = %q, want the prompt echoed back from stdin", res.Text)
	}
	// The CLI reports no token counts on this path; absent is not zero.
	if res.TokensIn != -1 || res.TokensOut != -1 {
		t.Errorf("tokens = %d/%d, want -1/-1 (absent, not zero)", res.TokensIn, res.TokensOut)
	}
}

func TestClaudeCLIEmptyStdoutIsAnError(t *testing.T) {
	// Exit 0 with nothing on stdout is not a whole answer, and
	// WHOLE_OR_ERROR means it must not land as one.
	b, err := newClaudeCLI(cliSpec(script(t, "exit 0"), nil))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	if _, err := b.Run(context.Background(), cliSpec("unused", nil)); err == nil {
		t.Fatal("Run landed an empty reply as success")
	}
}

func TestClaudeCLINonZeroExitCarriesStderr(t *testing.T) {
	b, err := newClaudeCLI(cliSpec(script(t, "echo 'not logged in' >&2; exit 3"), nil))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	_, err = b.Run(context.Background(), cliSpec("unused", nil))
	if err == nil {
		t.Fatal("Run reported success on a non-zero exit")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("error %q does not carry stderr; the diagnosis is the whole value", err)
	}
}

func TestClaudeCLITimeoutSaysSo(t *testing.T) {
	b, err := newClaudeCLI(cliSpec(script(t, "sleep 5"), map[string]string{"timeout_s": "1"}))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	_, err = b.Run(context.Background(), cliSpec("unused", nil))
	if err == nil {
		t.Fatal("Run reported success past its timeout")
	}
	// A killed subprocess otherwise surfaces as a bare "signal: killed"
	// that reads like a crash rather than a bound doing its job.
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error %q does not name the timeout", err)
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

func TestClaudeCLICarriesOtelAttribution(t *testing.T) {
	// Without this the hahod path would silently drop the per-repo,
	// per-PR spend attribution the shim already delivers.
	b, err := newClaudeCLI(cliSpec(
		script(t, `printf '%s' "$OTEL_RESOURCE_ATTRIBUTES"`),
		map[string]string{"otel_resource_attributes": "mesh.component=holden-judge,mesh.repo=kestrel,mesh.pr=2"},
	))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	res, err := b.Run(context.Background(), cliSpec("unused", nil))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Text, "mesh.repo=kestrel") || !strings.Contains(res.Text, "mesh.pr=2") {
		t.Errorf("child OTEL_RESOURCE_ATTRIBUTES = %q, want the spec's attribution", res.Text)
	}
}

func TestClaudeCLIAppendsRatherThanOverwritingAttribution(t *testing.T) {
	// hahod may be attributing its own work through this variable;
	// overwriting it would drop that silently.
	t.Setenv(otelResourceAttrs, "deployment.environment=fleet")
	b, err := newClaudeCLI(cliSpec(
		script(t, `printf '%s' "$OTEL_RESOURCE_ATTRIBUTES"`),
		map[string]string{"otel_resource_attributes": "mesh.repo=holden"},
	))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	res, err := b.Run(context.Background(), cliSpec("unused", nil))
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

func TestClaudeCLIRunsWithoutTools(t *testing.T) {
	// A payload is requestor-supplied text; for a ruling it carries design
	// docs from public repositories. A tool-bearing CLI can be steered by
	// that text into acting as the operator on this host. If this test ever
	// fails, an untrusted payload gained a way to act.
	b, err := newClaudeCLI(cliSpec(script(t, `printf '%s\n' "$@"; echo done`), nil))
	if err != nil {
		t.Fatalf("newClaudeCLI: %v", err)
	}
	res, err := b.Run(context.Background(), cliSpec("unused", nil))
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
