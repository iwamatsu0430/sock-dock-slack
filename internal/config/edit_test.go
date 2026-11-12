package config

import (
	"strings"
	"testing"
)

func TestReplaceRoutesKeepsTheRest(t *testing.T) {
	dir := t.TempDir()
	src := "# my config\nsettings:\n  language: ja # keep me\n\nroutes:\n  - name: old\n    channel: C0\n    match: x\n    handler: {type: command, cwd: " + dir + ", command: /bin/echo}\n\n# trailing note\nextra_comment_only: \n"
	// extra_comment_only is not a real key; strip it to keep the fixture valid YAML for ParseRaw later.
	src = strings.Replace(src, "extra_comment_only: \n", "", 1)
	routes := []*Route{{
		Name: "entry", Channel: "${CH}", Match: KeywordMatch("entry"),
		Handler: Handler{Type: TypeClaude, Cwd: dir, Instructions: "line one\nline two", AllowedTools: []string{"Bash(uv run x *)", "WebSearch"}},
	}}
	out, err := ReplaceRoutes([]byte(src), routes)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"# my config", "language: ja # keep me", "# trailing note", "name: entry"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "name: old") {
		t.Errorf("old route kept:\n%s", s)
	}
	raw, err := ParseRaw(out)
	if err != nil {
		t.Fatalf("result does not parse: %v\n%s", err, s)
	}
	r := raw.Routes[0]
	if r.Channel != "${CH}" || r.Handler.Instructions != "line one\nline two" || len(r.Handler.AllowedTools) != 2 || r.Match != KeywordMatch("entry") {
		t.Fatalf("round trip lost data: %+v\n%s", r.Handler, s)
	}
	if strings.Contains(s, "env_file") || strings.Contains(s, "allow_mcp") {
		t.Errorf("empty fields should be left out:\n%s", s)
	}
}

func TestReplaceRoutesEmptyAndMissing(t *testing.T) {
	out, err := ReplaceRoutes([]byte(MinimalConfig), nil)
	if err != nil || !strings.Contains(string(out), "routes: []") {
		t.Fatalf("%v\n%s", err, out)
	}
	out, err = ReplaceRoutes([]byte("settings:\n  language: en\n"), nil)
	if err != nil || !strings.Contains(string(out), "routes: []") || !strings.Contains(string(out), "language: en") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestSetSetting(t *testing.T) {
	cases := map[string]string{
		"settings:\n  language: ja\nroutes: []\n":               "settings:\n  claude_command: /opt/claude\n  language: ja\nroutes: []\n",
		"settings:\n  claude_command: claude # c\nroutes: []\n": "settings:\n  claude_command: /opt/claude\nroutes: []\n",
		"routes: []\n": "settings:\n  claude_command: /opt/claude\n\nroutes: []\n",
	}
	for in, want := range cases {
		out, err := SetSetting([]byte(in), "claude_command", "/opt/claude")
		if err != nil || string(out) != want {
			t.Errorf("SetSetting(%q) = %q, %v; want %q", in, out, err, want)
		}
	}
}

func TestKeywordMatch(t *testing.T) {
	cfg := &Config{Routes: []*Route{{Name: "r", Channel: "C", Match: KeywordMatch("entry")}}}
	if err := cfg.prepareMatchOnly(); err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string]string{"entry AB12 100件": "AB12 100件", "  entry": "", "entry\nmore lines": "more lines", "entryx": "-", "say entry": "-"} {
		r, g := cfg.Match("C", text)
		if want == "-" {
			if r != nil {
				t.Errorf("%q should not match", text)
			}
			continue
		}
		if r == nil || g["text"] != want {
			t.Errorf("%q: route=%v text=%q want %q", text, r, g["text"], want)
		}
	}
	for _, kw := range []string{"entry", "c++", "日本語"} {
		if got, ok := KeywordFromMatch(KeywordMatch(kw)); !ok || got != kw {
			t.Errorf("round trip %q -> %q %v", kw, got, ok)
		}
	}
	if _, ok := KeywordFromMatch(`^echo\s+(?P<body>.+)$`); ok {
		t.Error("a custom regex is not a keyword")
	}
}
