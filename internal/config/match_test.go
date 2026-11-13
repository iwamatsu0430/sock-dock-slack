package config

import "testing"

func TestConditions(t *testing.T) {
	cases := []struct {
		route Route
		text  string
		want  string // the "text" group, or "-" for no match
	}{
		{Route{StartsWith: "entry"}, "entry AB12 100件", "AB12 100件"},
		{Route{StartsWith: "entry"}, "Entry AB12", "AB12"},
		{Route{StartsWith: "entry"}, "ＥＮＴＲＹ　１００件", "１００件"},
		{Route{StartsWith: "entry"}, "<@UBOT> entry go", "go"},
		{Route{StartsWith: "entry"}, "<@UBOT|app> entry go", "go"},
		{Route{StartsWith: "entry"}, "<@UOTHER> entry go", "-"},
		{Route{StartsWith: "entry"}, "entry:go", ":go"},
		{Route{StartsWith: "entry"}, "entryx", "-"},
		{Route{StartsWith: "entry"}, "say entry", "-"},
		{Route{StartsWith: "相談"}, "相談したい", "したい"},
		{Route{StartsWith: "please review"}, "Please   review this", "this"},
		{Route{StartsWith: "a&b"}, "a&amp;b now", "now"},
		{Route{Contains: "entry"}, "new entry here", "new entry here"},
		{Route{Contains: "entry"}, "entryx", "-"},
		{Route{Contains: "bug"}, "debug this", "-"},
		{Route{Contains: "相談"}, "ちょっと相談です", "ちょっと相談です"},
		{Route{Contains: "c++"}, "about C++ code", "about C++ code"},
		{Route{AllPosts: true}, "<@UBOT> anything", "anything"},
	}
	for _, c := range cases {
		r := c.route
		r.Name, r.Channel = "r", "C"
		cfg := &Config{Routes: []*Route{&r}}
		if err := cfg.prepareMatchOnly(); err != nil {
			t.Fatal(err)
		}
		got, g := cfg.MatchWith("C", c.text, MatchOptions{BotUserID: "UBOT"})
		switch {
		case c.want == "-" && got != nil:
			t.Errorf("%s %q: should not match", r.Pattern(), c.text)
		case c.want != "-" && (got == nil || g["text"] != c.want):
			t.Errorf("%s %q: got %v text=%q, want %q", r.Pattern(), c.text, got != nil, g["text"], c.want)
		}
	}
}

func TestRegexIsCaseSensitive(t *testing.T) {
	cfg := &Config{Routes: []*Route{{Name: "r", Channel: "C", Match: `^deploy (?P<env>\w+)$`}}}
	if err := cfg.prepareMatchOnly(); err != nil {
		t.Fatal(err)
	}
	if r, g := cfg.Match("C", "deploy prod"); r == nil || g["env"] != "prod" {
		t.Errorf("got %v %v", r, g)
	}
	if r, _ := cfg.Match("C", "Deploy prod"); r != nil {
		t.Error("a regex should be case sensitive")
	}
}

func TestMatchOrderAndSkip(t *testing.T) {
	cfg := &Config{Routes: []*Route{
		{Name: "all", Channel: "C", AllPosts: true},
		{Name: "entry", Channel: "C", StartsWith: "entry"},
		{Name: "other", Channel: "D", StartsWith: "entry"},
	}}
	if err := cfg.prepareMatchOnly(); err != nil {
		t.Fatal(err)
	}
	if r, _ := cfg.Match("C", "entry 1"); r == nil || r.Name != "all" {
		t.Errorf("the first rule should win, got %v", r)
	}
	skip := func(name string) bool { return name == "all" }
	if r, _ := cfg.MatchWith("C", "entry 1", MatchOptions{Skip: skip}); r == nil || r.Name != "entry" {
		t.Errorf("a rule that is off should be passed over, got %v", r)
	}
	if r, _ := cfg.Match("D", "entry 1"); r == nil || r.Name != "other" {
		t.Errorf("channels are separate, got %v", r)
	}
}

func TestConditionProblems(t *testing.T) {
	for _, r := range []Route{{}, {StartsWith: "a", Contains: "b"}, {Match: "("}, {StartsWith: "  "}} {
		if p := r.conditionProblem(); p == "" {
			t.Errorf("%+v: want a problem", r)
		}
	}
}

func TestShadowedBy(t *testing.T) {
	cases := []struct {
		earlier, later Route
		want           bool
	}{
		{Route{AllPosts: true}, Route{StartsWith: "review"}, true},
		{Route{AllPosts: true}, Route{Match: "x"}, true},
		{Route{StartsWith: "entry"}, Route{StartsWith: "Entry"}, true},
		{Route{StartsWith: "entry"}, Route{StartsWith: "entry buy"}, true},
		{Route{StartsWith: "entry"}, Route{StartsWith: "entryx"}, false},
		{Route{StartsWith: "entry buy"}, Route{StartsWith: "entry"}, false},
		{Route{Contains: "相談"}, Route{StartsWith: "相談したい"}, true},
		{Route{Contains: "entry"}, Route{Contains: "new entry"}, true},
		{Route{StartsWith: "entry"}, Route{Contains: "entry"}, false},
		{Route{Match: "^x"}, Route{Match: "^x"}, true},
		{Route{Match: "^x"}, Route{StartsWith: "x"}, false},
		{Route{StartsWith: "x"}, Route{AllPosts: true}, false},
	}
	for _, c := range cases {
		c.earlier.Channel, c.later.Channel = "C", "C"
		if got := ShadowedBy(&c.earlier, &c.later); got != c.want {
			t.Errorf("%s before %s: got %v, want %v", c.earlier.Kind()+":"+c.earlier.Pattern(), c.later.Kind()+":"+c.later.Pattern(), got, c.want)
		}
	}
	a, b := Route{Channel: "C", AllPosts: true}, Route{Channel: "D", StartsWith: "x"}
	if ShadowedBy(&a, &b) {
		t.Error("different channels never shadow")
	}
}
