package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestParseExpandsAndMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "SLACK_ENTRY_CHANNEL_ID=C111\n# comment\nexport MODEL='opus'\n", 0o600)
	writeFile(t, filepath.Join(dir, "prompts/entry.md"), "hi", 0o644)
	writeFile(t, filepath.Join(dir, "handler.sh"), "#!/bin/sh\n", 0o755)
	t.Setenv("SD_TEST_DIR", dir)

	cfg, err := Parse([]byte(`
settings:
  claude_command: /bin/echo
routes:
  - name: web-app-entry
    channel: ${SLACK_ENTRY_CHANNEL_ID}
    match: '^\s*entry\s+(?P<code>[0-9A-Za-z０-９Ａ-Ｚａ-ｚ]{4,5})(?P<rest>.*)$'
    handler:
      type: claude
      cwd: ${SD_TEST_DIR}
      env_file: .env
      prompt: prompts/entry.md
      model: ${MODEL}
      allowed_tools: ["Bash(npm test *)", "WebSearch"]
  - name: echo
    channel: C222
    match: '^echo (?P<body>.+)$'
    handler:
      type: command
      cwd: ${SD_TEST_DIR}
      command: ./handler.sh
`), filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Route("web-app-entry")
	if r.Channel != "C111" || r.Handler.Model != "opus" {
		t.Fatalf("expansion failed: channel=%q model=%q", r.Channel, r.Handler.Model)
	}
	if r.Handler.Prompt != filepath.Join(dir, "prompts/entry.md") {
		t.Fatalf("prompt not resolved: %q", r.Handler.Prompt)
	}
	if r.Handler.TimeoutSec != DefaultClaudeTimeoutSec {
		t.Fatalf("default timeout not applied: %d", r.Handler.TimeoutSec)
	}
	if got := cfg.Route("echo").Handler.Command; len(got) != 1 || got[0] != "./handler.sh" {
		t.Fatalf("string command not parsed as list: %v", got)
	}

	cases := []struct {
		channel, text, route, code string
	}{
		{"C111", "entry 4321 100件を確認して", "web-app-entry", "4321"},
		{"C111", "  entry AB12", "web-app-entry", "AB12"},
		{"C111", "entry ＡＢ１２", "web-app-entry", "ＡＢ１２"},
		{"C111", "entry あいうえお", "", ""},
		{"C222", "entry 4321", "", ""},
		{"C222", "echo hello", "echo", ""},
	}
	for _, c := range cases {
		r, groups := cfg.Match(c.channel, c.text)
		got := ""
		if r != nil {
			got = r.Name
		}
		if got != c.route {
			t.Errorf("Match(%q, %q) = %q, want %q", c.channel, c.text, got, c.route)
			continue
		}
		if c.code != "" && groups["code"] != c.code {
			t.Errorf("Match(%q).code = %q, want %q", c.text, groups["code"], c.code)
		}
	}
	if _, groups := cfg.Match("C111", "entry 4321 100件"); groups["rest"] != " 100件" {
		t.Errorf("rest = %q", groups["rest"])
	}
}

func TestValidationCollectsProblems(t *testing.T) {
	dir := t.TempDir()
	_, err := Parse([]byte(`
routes:
  - name: a
    channel: ${NOPE_NOT_SET}
    match: '('
    handler:
      type: command
      cwd: `+dir+`
  - name: a
    channel: C1
    match: x
    handler:
      type: whatever
      cwd: relative/path
`), "cfg.yaml")
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	joined := strings.Join(ve.Problems, "\n")
	for _, want := range []string{"${NOPE_NOT_SET} is not set", "match:", "handler.command is required", "duplicate name", "unknown handler.type", "absolute path"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
}

func TestUnknownFieldRejected(t *testing.T) {
	if _, err := Parse([]byte("setings:\n  max_concurrency: 2\n"), "cfg.yaml"); err == nil {
		t.Fatal("typo in a key should be rejected")
	}
}

func TestDotenvQuotes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".env")
	writeFile(t, p, "A=\"x y\\n\"\nB='raw $x'\nC=plain # trailing\nexport D=1\n", 0o600)
	m, err := ReadDotenv(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "x y\n", "B": "raw $x", "C": "plain", "D": "1"}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %q, want %q", k, m[k], v)
		}
	}
}

func TestRender(t *testing.T) {
	if got := Render("{route} took {timeout_sec}s", "r1", 30); got != "r1 took 30s" {
		t.Fatal(got)
	}
}
