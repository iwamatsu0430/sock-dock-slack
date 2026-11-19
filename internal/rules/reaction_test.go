package rules

import (
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

func TestReactionForm(t *testing.T) {
	base := &config.Route{Match: "old", MentionOnly: true}
	r := FromForm(Form{Name: "x", Channel: "C0123ABCD", Trigger: "reaction", Keyword: " :Eyes: ", ReactionRemoved: true, MentionOnly: true, Type: "claude", Folder: "/tmp"}, base)
	if r.Kind() != config.KindReaction || r.Reaction != "eyes" || !r.ReactionRemoved || r.MentionOnly || r.Match != "" {
		t.Fatalf("route = %+v", r)
	}
	f := ToForm(r)
	if f.Trigger != "reaction" || f.Keyword != ":eyes:" || !f.ReactionRemoved {
		t.Errorf("form = %+v", f)
	}
	// Changing the condition drops what only a reaction rule has.
	f.Trigger, f.Keyword = "starts_with", "go"
	if r := FromForm(f, r); r.Reaction != "" || r.ReactionRemoved || r.StartsWith != "go" {
		t.Errorf("after changing the condition: %+v", r)
	}

	code := func(kw string) string {
		for _, p := range Validate(Form{Name: "x", Channel: "C0123ABCD", Trigger: "reaction", Keyword: kw, Type: "claude", Folder: t.TempDir()}, &config.Config{}) {
			if p.Field == "keyword" {
				return p.Code
			}
		}
		return ""
	}
	for kw, want := range map[string]string{":eyes:": "", "eyes": "", "+1": "", "": "required", "::": "bad_reaction", "two words": "bad_reaction", ":a::b:": "bad_reaction"} {
		if got := code(kw); got != want {
			t.Errorf("keyword %q: got %q, want %q", kw, got, want)
		}
	}
}

func TestReactionRulesInList(t *testing.T) {
	dir := t.TempDir()
	h := config.Handler{Type: config.TypeCommand, Cwd: dir, Command: config.StringList{"/bin/echo"}}
	raw := &config.Config{Routes: []*config.Route{
		{Name: "all", Channel: "C0123ABCD", AllPosts: true, Handler: h},
		{Name: "first", Channel: "C0123ABCD", Reaction: "ticket", Handler: h},
		{Name: "second", Channel: "C0123ABCD", Reaction: "ticket", Handler: h},
	}}
	rows := List(raw, nil)
	if rows[1].ShadowedBy != nil {
		t.Errorf("an all-posts rule must not hide a reaction rule: %+v", rows[1].ShadowedBy)
	}
	if s := rows[2].ShadowedBy; s == nil || s.Trigger != "reaction" || s.Keyword != ":ticket:" {
		t.Errorf("second reaction rule: shadowed by %+v", s)
	}
}
