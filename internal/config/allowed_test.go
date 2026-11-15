package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestAllowedUsersMatching(t *testing.T) {
	only := Route{Name: "only", Channel: "C", StartsWith: "review", AllowedUsers: []string{"UALEX"}}
	all := Route{Name: "all", Channel: "C", AllPosts: true}
	cfg := &Config{Routes: []*Route{&only, &all}}
	if err := cfg.prepareMatchOnly(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		o    MatchOptions
		want string
	}{
		{MatchOptions{ByUser: true, User: "UALEX"}, "only"},
		// Someone else: the restricted rule is passed over, like a rule that is off.
		{MatchOptions{ByUser: true, User: "USAM"}, "all"},
		{MatchOptions{ByUser: true, User: ""}, "all"},
		// Without ByUser, who posted is not considered (sockdock match without --user).
		{MatchOptions{}, "only"},
	}
	for _, c := range cases {
		r, _ := cfg.MatchWith("C", "review x", c.o)
		if r == nil || r.Name != c.want {
			t.Errorf("%+v: got %v, want %s", c.o, r, c.want)
		}
	}
	if !all.Allows("") || !only.Allows("UALEX") || only.Allows("USAM") {
		t.Error("Allows is wrong")
	}
}

func TestShadowedByRestricted(t *testing.T) {
	upper := &Route{Channel: "C", AllPosts: true, AllowedUsers: []string{"UALEX"}}
	lower := &Route{Channel: "C", StartsWith: "review"}
	if ShadowedBy(upper, lower) {
		t.Error("a restricted rule lets others' posts through, so it should not shadow")
	}
	upper.AllowedUsers = nil
	if !ShadowedBy(upper, lower) {
		t.Error("an all-posts rule for everyone shadows the rules below")
	}
}

func TestAllowedUsersValidation(t *testing.T) {
	dir := t.TempDir()
	base := "routes:\n  - name: r\n    channel: C111\n    starts_with: hi\n%s    handler: {type: command, cwd: " + dir + ", command: /bin/echo}\n"
	cases := map[string]string{
		"    allowed_users: []\n":          "allowed_users is empty",
		"    allowed_users: [alex]\n":      "is not a Slack member ID",
		"    allowed_users: [U0123ABCD]\n": "",
		"":                                 "",
	}
	for field, want := range cases {
		cfg, err := Parse([]byte(strings.Replace(base, "%s", field, 1)), filepath.Join(dir, "config.yaml"))
		switch {
		case want == "" && err != nil:
			t.Errorf("%q: unexpected error %v", field, err)
		case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
			t.Errorf("%q: want error containing %q, got %v", field, want, err)
		case want == "" && field != "" && !cfg.Routes[0].Restricted():
			t.Errorf("%q: should be restricted", field)
		}
	}
}

func TestRenderAllowedUsers(t *testing.T) {
	out, err := RenderRoutes([]*Route{{Name: "r", Channel: "C1", StartsWith: "hi", AllowedUsers: []string{"U1AB", "U2CD"},
		Handler: Handler{Type: TypeCommand, Cwd: "/tmp", Command: []string{"/bin/echo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "allowed_users: [U1AB, U2CD]") {
		t.Errorf("want flow style allowed_users:\n%s", out)
	}
	out, _ = RenderRoutes([]*Route{{Name: "r", Channel: "C1", StartsWith: "hi", Handler: Handler{Type: TypeCommand, Cwd: "/tmp"}}})
	if strings.Contains(string(out), "allowed_users") {
		t.Errorf("everyone: the key should be left out:\n%s", out)
	}
}
