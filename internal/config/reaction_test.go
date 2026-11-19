package config

import (
	"strings"
	"testing"
)

func TestNormalizeReaction(t *testing.T) {
	for in, want := range map[string]string{
		"eyes": "eyes", ":eyes:": "eyes", " :Eyes: ": "eyes", "+1::skin-tone-2": "+1", ":+1::skin-tone-6:": "+1",
		"thumbsup": "+1", ":thumbsup::skin-tone-3:": "+1", "Thumbsdown": "-1", "raised_hand": "hand", "mag": "mag",
		"white_check_mark": "white_check_mark", "": "", "::": "",
	} {
		if got := NormalizeReaction(in); got != want {
			t.Errorf("NormalizeReaction(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReactionCondition(t *testing.T) {
	r := Route{Name: "r", Channel: "C", Reaction: ":Eyes:", ReactionRemoved: true}
	if r.Kind() != KindReaction || r.Pattern() != "eyes" {
		t.Fatalf("kind %q, pattern %q", r.Kind(), r.Pattern())
	}
	if got := r.Condition(); got != "reaction: eyes, reaction_removed" {
		t.Errorf("condition = %q", got)
	}
	if p := r.conditionProblem(); p != "" {
		t.Errorf("problem: %s", p)
	}
	for _, bad := range []Route{{Reaction: "eyes", StartsWith: "x"}, {Reaction: "two words"}, {Reaction: "::"}} {
		if p := bad.conditionProblem(); p == "" {
			t.Errorf("%+v: want a problem", bad)
		}
	}
	// A reaction rule never matches a post's text.
	cfg := &Config{Routes: []*Route{&r}}
	if got, _ := cfg.MatchWith("C", "eyes :eyes:", MatchOptions{}); got != nil {
		t.Error("a reaction rule matched a post's text")
	}
}

func TestMatchReaction(t *testing.T) {
	cfg := &Config{Routes: []*Route{
		{Name: "text", Channel: "C", AllPosts: true},
		{Name: "limited", Channel: "C", Reaction: "eyes", AllowedUsers: []string{"U1"}},
		{Name: "open", Channel: "C", Reaction: "eyes"},
		{Name: "dm", Channel: ChannelDM, Reaction: "eyes"},
		{Name: "thumbs", Channel: "C", Reaction: ":thumbsup:"},
	}}
	name := func(r *Route) string {
		if r == nil {
			return ""
		}
		return r.Name
	}
	cases := []struct {
		channel, reaction string
		o                 MatchOptions
		want              string
	}{
		{"C", "eyes", MatchOptions{ByUser: true, User: "U1"}, "limited"},
		{"C", "eyes::skin-tone-3", MatchOptions{ByUser: true, User: "U1"}, "limited"},
		{"C", "eyes", MatchOptions{ByUser: true, User: "U2"}, "open"},
		{"C", "eyes", MatchOptions{Skip: func(n string) bool { return n == "limited" }, ByUser: true, User: "U1"}, "open"},
		{"C", "mag", MatchOptions{}, ""},
		{"X", "eyes", MatchOptions{}, ""},
		{"D123", "eyes", MatchOptions{IM: true}, "dm"},
		{"C", "+1", MatchOptions{}, "thumbs"},
		{"C", "+1::skin-tone-2", MatchOptions{}, "thumbs"},
	}
	for _, c := range cases {
		if got := name(cfg.MatchReaction(c.channel, c.reaction, c.o)); got != c.want {
			t.Errorf("%s %s by %q: got %q, want %q", c.channel, c.reaction, c.o.User, got, c.want)
		}
	}
}

func TestReactionShadowing(t *testing.T) {
	cases := []struct {
		earlier, later Route
		want           bool
	}{
		{Route{Reaction: "eyes"}, Route{Reaction: ":eyes:"}, true},
		{Route{Reaction: "eyes"}, Route{Reaction: "mag"}, false},
		{Route{Reaction: "eyes", AllowedUsers: []string{"U1"}}, Route{Reaction: "eyes"}, false},
		// "All posts" does not see other apps' posts, which a reaction rule
		// still runs for.
		{Route{AllPosts: true}, Route{Reaction: "eyes"}, false},
		{Route{Reaction: "eyes"}, Route{AllPosts: true}, false},
		{Route{Reaction: "eyes"}, Route{StartsWith: "eyes"}, false},
	}
	for _, c := range cases {
		c.earlier.Channel, c.later.Channel = "C", "C"
		if got := ShadowedBy(&c.earlier, &c.later); got != c.want {
			t.Errorf("%s before %s: got %v, want %v", c.earlier.Condition(), c.later.Condition(), got, c.want)
		}
	}
}

func TestReactionConfig(t *testing.T) {
	dir := t.TempDir()
	parse := func(route string) error {
		_, err := Parse([]byte("routes:\n  - name: r\n    channel: C1\n"+route+"\n    handler: {type: command, cwd: "+dir+", command: /bin/echo}\n"), dir+"/config.yaml")
		return err
	}
	if err := parse("    reaction: ':eyes:'\n    reaction_removed: true"); err != nil {
		t.Errorf("valid reaction rule: %v", err)
	}
	for route, want := range map[string]string{
		"    reaction: eyes\n    mention_only: true":      "mention_only can't be used with reaction",
		"    starts_with: go\n    reaction_removed: true": "reaction_removed needs reaction",
		"    reaction: eyes\n    starts_with: go":         "use only one of",
	} {
		if err := parse(route); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", route, err, want)
		}
	}

	out, err := RenderRoutes([]*Route{
		{Name: "a", Channel: "C1", Reaction: "eyes", ReactionRemoved: true, Handler: Handler{Type: TypeCommand, Cwd: dir}},
		{Name: "b", Channel: "C1", StartsWith: "go", ReactionRemoved: true, Handler: Handler{Type: TypeCommand, Cwd: dir}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); !strings.Contains(got, "reaction: eyes\n") || strings.Count(got, "reaction_removed: true") != 1 {
		t.Errorf("rendered routes:\n%s", got)
	}
}
