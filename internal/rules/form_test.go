package rules

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

func TestFormRoundTrip(t *testing.T) {
	in := Form{
		Name: "entry", Channel: "https://example.slack.com/archives/C0123ABCD", Trigger: "starts_with", Keyword: "entry",
		Type: "claude", Folder: "~/proj", Instructions: "Read prompts/entry.md first.",
		AllowWeb: true, Commands: []string{"npm test"}, OtherTools: []string{"Write(data/tmp/**)"}, Model: "opus",
	}
	r := FromForm(in, nil)
	if r.Channel != "C0123ABCD" {
		t.Errorf("channel = %q", r.Channel)
	}
	want := []string{"WebSearch", "WebFetch", "Bash(npm test *)", "Write(data/tmp/**)"}
	if !slices.Equal(r.Handler.AllowedTools, want) {
		t.Errorf("allowed_tools = %v", r.Handler.AllowedTools)
	}
	out := ToForm(r)
	if out.Trigger != "starts_with" || out.Keyword != "entry" || !out.AllowWeb || out.AllowEdit ||
		!slices.Equal(out.Commands, []string{"npm test"}) || !slices.Equal(out.OtherTools, []string{"Write(data/tmp/**)"}) {
		t.Fatalf("round trip = %+v", out)
	}

	cmd := FromForm(Form{Name: "c", Channel: "C0123ABCD", Trigger: "match", Keyword: `^echo (?P<b>.+)`, Type: "command",
		Folder: "/tmp", Program: "./run.sh", Args: `--name "two words" x`}, nil)
	if !slices.Equal(cmd.Handler.Command, []string{"./run.sh", "--name", "two words", "x"}) {
		t.Errorf("command = %q", cmd.Handler.Command)
	}
	if f := ToForm(cmd); f.Trigger != "match" || f.Keyword != `^echo (?P<b>.+)` || f.Args != `--name "two words" x` {
		t.Errorf("command form = %+v", f)
	}
}

func TestFormKeepsUneditedFields(t *testing.T) {
	base := &config.Route{Name: "x", Handler: config.Handler{Type: "claude", EnvFile: ".env", ExtraArgs: []string{"--verbose"}}}
	r := FromForm(Form{Name: "x", Channel: "C0123ABCD", Trigger: "starts_with", Keyword: "go", Type: "claude", Folder: "/tmp"}, base)
	if r.Handler.EnvFile != ".env" || len(r.Handler.ExtraArgs) != 1 {
		t.Fatalf("lost fields: %+v", r.Handler)
	}
}

func TestValidateForm(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	os.WriteFile(script, []byte("#!/bin/sh\n"), 0o644) // not executable
	raw := &config.Config{Routes: []*config.Route{{Name: "taken"}}}
	codes := func(f Form) map[string]string {
		m := map[string]string{}
		for _, p := range Validate(f, raw) {
			m[p.Field] = p.Code
		}
		return m
	}
	got := codes(Form{Name: "taken", Channel: "general", Trigger: "starts_with", Keyword: "  ", Type: "command", Folder: filepath.Join(dir, "nope"), Program: script})
	want := map[string]string{"name": "duplicate", "channel": "bad_channel", "keyword": "required", "folder": "not_dir", "program": "not_executable"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	ok := codes(Form{OriginalName: "taken", Name: "taken", Channel: "C0123ABCD", Trigger: "starts_with", Keyword: "go", Type: "claude", Folder: dir})
	if len(ok) != 0 {
		t.Errorf("valid form reported %v", ok)
	}
}

func TestFormConditions(t *testing.T) {
	for _, trig := range []string{"starts_with", "contains", "match", "all_posts"} {
		r := FromForm(Form{Name: "x", Channel: "C0123ABCD", Trigger: trig, Keyword: "go", Type: "claude", Folder: "/tmp"}, &config.Route{Match: "old"})
		if r.Kind() != trig {
			t.Errorf("%s: kind = %q (%+v)", trig, r.Kind(), r)
		}
		if f := ToForm(r); f.Trigger != trig {
			t.Errorf("%s: form trigger = %q", trig, f.Trigger)
		}
	}
	ps := Validate(Form{Name: "x", Channel: "C0123ABCD", Trigger: "match", Keyword: "(", Type: "claude", Folder: t.TempDir()}, &config.Config{})
	if len(ps) != 1 || ps[0].Field != "keyword" || ps[0].Code != "bad_regex" {
		t.Errorf("bad regex: %v", ps)
	}
	if ps := Validate(Form{Name: "x", Channel: "C0123ABCD", Trigger: "all_posts", Type: "claude", Folder: t.TempDir()}, &config.Config{}); len(ps) != 0 {
		t.Errorf("all posts needs no keyword: %v", ps)
	}
}

// A codex rule keeps its permissions in allow_web / allow_edit, never in
// Claude Code's allowed_tools (docs/design.md §3.8).
func TestCodexForm(t *testing.T) {
	in := Form{Name: "cx", Channel: "C0123ABCD", Trigger: "starts_with", Keyword: "cx", Type: "codex", Folder: "~/proj",
		Instructions: "Be brief.", AllowWeb: true, Commands: []string{"npm test"}, Model: "gpt-6-luna"}
	r := FromForm(in, nil)
	h := r.Handler
	if h.Type != "codex" || !h.AllowWeb || h.AllowEdit || h.AllowedTools != nil || h.Model != "gpt-6-luna" || h.Instructions != "Be brief." {
		t.Fatalf("handler = %+v", h)
	}
	out := ToForm(r)
	if out.Type != "codex" || !out.AllowWeb || out.AllowEdit || len(out.Commands) != 0 {
		t.Errorf("form = %+v", out)
	}
	// Back to Claude Code: the permissions become allowed_tools again.
	in.Type = "claude"
	back := FromForm(in, r)
	if back.Handler.AllowWeb || !slices.Contains(back.Handler.AllowedTools, "WebSearch") || !slices.Contains(back.Handler.AllowedTools, "Bash(npm test *)") {
		t.Errorf("claude handler = %+v", back.Handler)
	}
	if OpenToAll(Form{Type: "codex"}) != "commands" || OpenToAll(Form{Type: "codex", Restricted: true}) != "" {
		t.Error("Codex can always run commands")
	}
}
