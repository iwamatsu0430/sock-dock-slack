package handler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// fakeCodex records its argv and stdin, then prints `codex exec --json`
// events (taken from Codex 0.169.0 on 2026-11-15) according to FAKE_MODE.
const fakeCodex = `#!/bin/sh
printf '%s\n' "$@" > "$RECORD_DIR/args"
cat > "$RECORD_DIR/stdin"
resume=no
for a in "$@"; do [ "$a" = "resume" ] && resume=yes; done
case "$FAKE_MODE" in
  ok)
    echo '{"type":"thread.started","thread_id":"thread-new"}'
    echo '{"type":"turn.started"}'
    echo '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I will look."}}'
    echo '{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"ls","aggregated_output":"a\n","exit_code":0,"status":"completed"}}'
    echo '{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"**done**"}}'
    echo '{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":2}}' ;;
  resume-fail)
    if [ "$resume" = yes ]; then
      echo 'Error: thread/resume: thread/resume failed: no rollout found for thread id gone (code -32600)' >&2; exit 1
    fi
    echo '{"type":"thread.started","thread_id":"thread-fresh"}'
    echo '{"type":"turn.started"}'
    echo '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"fresh"}}'
    echo '{"type":"turn.completed","usage":{}}' ;;
  auth)
    echo '{"type":"thread.started","thread_id":"t"}'
    echo '{"type":"turn.started"}'
    echo '{"type":"error","message":"Reconnecting... 5/5 (unexpected status 401 Unauthorized: Missing bearer or basic authentication in header)"}'
    echo '{"type":"turn.failed","error":{"message":"unexpected status 401 Unauthorized: Missing bearer or basic authentication in header"}}'
    exit 1 ;;
  error)
    echo '{"type":"thread.started","thread_id":"t"}'
    echo '{"type":"turn.failed","error":{"message":"stream disconnected"}}'
    exit 1 ;;
esac
`

func setupCodex(t *testing.T, mode string) (*config.Route, string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-codex")
	if err := os.WriteFile(script, []byte(fakeCodex), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(prompt, []byte("Rule prompt file."), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &config.Route{
		Name: "r",
		Handler: config.Handler{
			Type: config.TypeCodex, Cwd: dir, Prompt: prompt, Instructions: "Answer \"briefly\".",
			AllowWeb: true, Model: "gpt-6-luna", TimeoutSec: 10, Command: config.StringList{script},
		},
		Env: map[string]string{"FAKE_MODE": mode, "RECORD_DIR": dir},
	}
	return r, dir
}

func TestCodexArgsAndResult(t *testing.T) {
	r, dir := setupCodex(t, "ok")
	gw := filepath.Join(dir, "gw.md")
	if err := os.WriteFile(gw, []byte("Gateway rules."), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := RunCodex(context.Background(), r, input, CodexOptions{
		Command: r.Handler.Command, SystemPromptFile: gw, SessionID: "thread-old",
	}, Env{Path: os.Getenv("PATH")})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "**done**" || res.SessionID != "thread-new" || res.ResumeFailed {
		t.Fatalf("result = %+v", res)
	}
	args := readLines(t, filepath.Join(dir, "args"))
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"exec resume --json --skip-git-repo-check --ignore-user-config --disable apps",
		`-c approval_policy="never"`, `-c default_permissions="sockdock"`,
		`-c web_search="live"`, "-m gpt-6-luna",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q:\n%s", want, joined)
		}
	}
	if args[len(args)-2] != "thread-old" || args[len(args)-1] != "-" {
		t.Errorf("thread id and - must come last: %q", args[len(args)-2:])
	}
	if !slices.Contains(args, `developer_instructions="Gateway rules.\n\nRule prompt file.\n\nAnswer \"briefly\"."`) {
		t.Errorf("developer instructions missing or badly quoted:\n%s", joined)
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	if !strings.Contains(string(stdin), `permalink="https://x/p1"`) || strings.Count(string(stdin), "</slack_message>") != 1 {
		t.Errorf("prompt = %s", stdin)
	}
}

func TestCodexFilesystem(t *testing.T) {
	home := "/Users/me"
	got := codexFilesystem(false, []string{"/Users/me/.local/bin", "w:/Users/me/Library/Caches/mise"}, home)
	want := `{"/"="read", "/Users/me"="deny", "/Users/me/.local/bin"="read", "/Users/me/Library/Caches/mise"="write", ":workspace_roots"={"."="read"}}`
	if got != want {
		t.Errorf("read-only:\n got %s\nwant %s", got, want)
	}
	if got := codexFilesystem(true, nil, home); !strings.HasSuffix(got, `":workspace_roots"={"."="write"}}`) {
		t.Errorf("edit: %s", got)
	}
}

func TestCodexReadable(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "share", "mise", "installs", "codex", "1.0", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	files := filepath.Join(home, "data", "turn-1")
	path := strings.Join([]string{"/opt/homebrew/bin", home, filepath.Join(home, ".local", "share", "mise", "shims")}, ":")
	got := codexReadable(filepath.Join(bin, "codex"), files, path, home)
	for _, want := range []string{
		filepath.Join(home, ".local", "share", "mise", "installs", "codex", "1.0"),
		files,
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, ".config", "mise"),
		"w:" + filepath.Join(home, "Library", "Caches", "mise"),
	} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s in %q", want, got)
		}
	}
	for _, bad := range []string{home, "/opt/homebrew/bin"} {
		if slices.Contains(got, bad) {
			t.Errorf("%s must not be listed (home stays closed; outside home is readable anyway): %q", bad, got)
		}
	}
}

func TestCodexEditAndNoWeb(t *testing.T) {
	r, dir := setupCodex(t, "ok")
	r.Handler.AllowWeb, r.Handler.AllowEdit, r.Handler.AllowMCP = false, true, true
	if _, err := RunCodex(context.Background(), r, input, CodexOptions{Command: r.Handler.Command}, Env{Path: os.Getenv("PATH")}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(readLines(t, filepath.Join(dir, "args")), " ")
	if !strings.Contains(joined, `web_search="disabled"`) || !strings.Contains(joined, `{"."="write"}`) {
		t.Errorf("args = %s", joined)
	}
	if strings.Contains(joined, "--ignore-user-config") || strings.Contains(joined, " resume ") {
		t.Errorf("allow_mcp keeps the user's config; a first turn does not resume: %s", joined)
	}
}

func TestCodexResumeFallback(t *testing.T) {
	r, _ := setupCodex(t, "resume-fail")
	res, err := RunCodex(context.Background(), r, input, CodexOptions{Command: r.Handler.Command, SessionID: "gone"}, Env{Path: os.Getenv("PATH")})
	if err != nil {
		t.Fatal(err)
	}
	if !res.ResumeFailed || res.SessionID != "thread-fresh" || res.Text != "fresh" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCodexSignedOutAndError(t *testing.T) {
	r, _ := setupCodex(t, "auth")
	_, err := RunCodex(context.Background(), r, input, CodexOptions{Command: r.Handler.Command}, Env{Path: os.Getenv("PATH")})
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Reason != ReasonCodexSignedOut {
		t.Fatalf("auth err = %v", err)
	}
	r, _ = setupCodex(t, "error")
	_, err = RunCodex(context.Background(), r, input, CodexOptions{Command: r.Handler.Command}, Env{Path: os.Getenv("PATH")})
	if !errors.As(err, &ee) || !strings.Contains(ee.Error(), "stream disconnected") {
		t.Fatalf("err = %v", err)
	}
}

func TestCodexSignedIn(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "codex")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n[ \"$1 $2\" = \"login status\" ] || exit 9\nexit \"$CODE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string][2]bool{"0": {true, true}, "1": {false, true}, "2": {false, false}} {
		t.Setenv("CODE", code)
		in, ok := CodexSignedIn(context.Background(), []string{script}, os.Getenv("PATH"))
		if in != want[0] || ok != want[1] {
			t.Errorf("exit %s: signedIn=%v ok=%v", code, in, ok)
		}
	}
}

func TestTOMLString(t *testing.T) {
	if got := tomlString("a\"b\\c\nd\x01é"); got != `"a\"b\\c\nd\u0001é"` {
		t.Errorf("got %s", got)
	}
}
