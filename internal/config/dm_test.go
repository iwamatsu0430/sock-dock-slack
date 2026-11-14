package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDMRules(t *testing.T) {
	dm := Route{Name: "dm", Channel: ChannelDM, StartsWith: "todo"}
	ch := Route{Name: "ch", Channel: "C1", StartsWith: "todo"}
	cfg := &Config{Routes: []*Route{&ch, &dm}}
	if err := cfg.prepareMatchOnly(); err != nil {
		t.Fatal(err)
	}
	// A DM (any D… channel) goes to the rules of channel "dm".
	if r, g := cfg.MatchWith("D0ALEX", "todo milk", MatchOptions{IM: true}); r == nil || r.Name != "dm" || g["text"] != "milk" {
		t.Errorf("DM: got %v %v", r, g)
	}
	// Trying a post with channel "dm" does the same.
	if r, _ := cfg.MatchWith(ChannelDM, "todo milk", MatchOptions{}); r == nil || r.Name != "dm" {
		t.Errorf("channel dm: got %v", r)
	}
	// A channel post never reaches a DM rule.
	if r, _ := cfg.MatchWith("C1", "todo milk", MatchOptions{}); r == nil || r.Name != "ch" {
		t.Errorf("channel: got %v", r)
	}
	if r, _ := cfg.MatchWith("D0ALEX", "todo milk", MatchOptions{}); r != nil {
		t.Errorf("a D channel without IM should not match a channel rule: %v", r)
	}
}

func TestMentionOnly(t *testing.T) {
	mention := Route{Name: "mention", Channel: "C1", StartsWith: "review", MentionOnly: true}
	all := Route{Name: "all", Channel: "C1", AllPosts: true}
	cfg := &Config{Routes: []*Route{&mention, &all}}
	if err := cfg.prepareMatchOnly(); err != nil {
		t.Fatal(err)
	}
	o := MatchOptions{BotUserID: "UBOT"}
	cases := []struct {
		text, want, groupText string
	}{
		{"<@UBOT> review src/api.ts", "mention", "src/api.ts"},
		{"<@UBOT|dev-bot> review src/api.ts", "mention", "src/api.ts"},
		// A mention elsewhere in the post counts too.
		{"review src/api.ts <@UBOT>", "mention", "src/api.ts <@UBOT>"},
		// Without a mention the rule is passed over and the next one runs.
		{"review src/api.ts", "all", "review src/api.ts"},
		{"<@UOTHER> review src/api.ts", "all", "<@UOTHER> review src/api.ts"},
	}
	for _, c := range cases {
		r, g := cfg.MatchWith("C1", c.text, o)
		if r == nil || r.Name != c.want || g["text"] != c.groupText {
			t.Errorf("%q: got %v %v, want %s %q", c.text, r, g, c.want, c.groupText)
		}
	}
	// Mentioned stands in for a mention when the app's user ID is not known.
	if r, _ := cfg.MatchWith("C1", "review x", MatchOptions{Mentioned: true}); r == nil || r.Name != "mention" {
		t.Errorf("Mentioned: got %v", r)
	}
}

func TestShadowedByMentionOnly(t *testing.T) {
	upper := &Route{Channel: "C", AllPosts: true, MentionOnly: true}
	lower := &Route{Channel: "C", StartsWith: "review"}
	if ShadowedBy(upper, lower) {
		t.Error("a mention-only rule lets other posts through, so it should not shadow")
	}
	lower.MentionOnly = true
	if !ShadowedBy(upper, lower) {
		t.Error("both mention-only: the all-posts rule shadows")
	}
	upper.MentionOnly = false
	if !ShadowedBy(upper, lower) {
		t.Error("a rule for every post shadows a mention-only rule below")
	}
}

func TestDMValidation(t *testing.T) {
	dir := t.TempDir()
	base := "settings:\n%s\nroutes:\n  - name: r\n    channel: %s\n    starts_with: hi\n%s    handler: {type: command, cwd: " + dir + ", command: /bin/echo}\n"
	cases := []struct {
		settings, channel, field, want string
	}{
		{"  language: en", "dm", "", ""},
		{"  language: en", "dm", "    mention_only: true\n", "mention_only can't be used with channel: dm"},
		{"  language: en", "C111", "    mention_only: true\n", ""},
		{"  owner: U0123ABCD\n  notify_dm: true\n  notify_dm_failures: true", "C111", "", ""},
		{"  owner: alex", "C111", "", "settings.owner"},
	}
	for _, c := range cases {
		data := strings.Replace(strings.Replace(strings.Replace(base, "%s", c.settings, 1), "%s", c.channel, 1), "%s", c.field, 1)
		cfg, err := Parse([]byte(data), filepath.Join(dir, "config.yaml"))
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%+v: unexpected error %v", c, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%+v: want error containing %q, got %v", c, c.want, err)
		case c.want == "" && strings.Contains(c.settings, "owner") && (cfg.Settings.Owner != "U0123ABCD" || !cfg.Settings.NotifyDM || !cfg.Settings.NotifyDMFailures):
			t.Errorf("settings not read: %+v", cfg.Settings)
		}
	}
}

func TestRenderMentionOnly(t *testing.T) {
	out, err := RenderRoutes([]*Route{{Name: "r", Channel: "C1", StartsWith: "hi", MentionOnly: true,
		Handler: Handler{Type: TypeCommand, Cwd: "/tmp", Command: []string{"/bin/echo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "mention_only: true") {
		t.Errorf("want mention_only:\n%s", out)
	}
}

func TestSettingValueAndDelete(t *testing.T) {
	data := []byte("settings:\n  language: auto\n\nroutes: []\n")
	out, err := SetSettingValue(data, "notify_dm", true)
	if err != nil {
		t.Fatal(err)
	}
	out, _ = SetSettingValue(out, "owner", "U0123ABCD")
	c, err := ParseRaw(out)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Settings.NotifyDM || c.Settings.Owner != "U0123ABCD" {
		t.Errorf("got %+v from\n%s", c.Settings, out)
	}
	out = DeleteSetting(out, "owner")
	if strings.Contains(string(out), "owner") || !strings.Contains(string(out), "notify_dm: true") {
		t.Errorf("owner should be removed only:\n%s", out)
	}
	if got := DeleteSetting(out, "missing"); string(got) != string(out) {
		t.Error("deleting a missing key should change nothing")
	}
}
