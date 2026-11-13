package rules

import (
	"strings"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

func names(t *testing.T, content []byte) string {
	t.Helper()
	raw, err := config.ParseRaw(content)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range raw.Routes {
		out = append(out, r.Name+"@"+r.Channel)
	}
	return strings.Join(out, ",")
}

func sampleConfig(t *testing.T) ([]byte, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	src := "routes:\n" +
		"  - {name: a, channel: C0000AAA, starts_with: x, handler: {type: command, cwd: " + dir + ", command: /bin/echo}}\n" +
		"  - {name: b, channel: C0000BBB, starts_with: y, handler: {type: command, cwd: " + dir + ", command: /bin/echo}}\n" +
		"  - {name: c, channel: C0000AAA, starts_with: z, handler: {type: command, cwd: " + dir + ", command: /bin/echo}}\n"
	raw, err := config.ParseRaw([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return []byte(src), raw
}

func TestReorderChannelKeepsOtherChannels(t *testing.T) {
	data, raw := sampleConfig(t)
	out, err := ReorderChannel(data, raw, "C0000AAA", []string{"c", "a"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(t, out); got != "c@C0000AAA,b@C0000BBB,a@C0000AAA" {
		t.Errorf("order = %s", got)
	}
	for _, bad := range [][]string{{"a"}, {"a", "b"}, {"a", "a"}} {
		if _, err := ReorderChannel(data, raw, "C0000AAA", bad); err != ErrChanged {
			t.Errorf("%v: err = %v", bad, err)
		}
	}
}

func TestSaveMovesToBottomOfNewChannel(t *testing.T) {
	data, raw := sampleConfig(t)
	f := ToForm(raw.Route("a"))
	f.Channel = "C0000BBB"
	saved, err := Save(data, raw, f)
	if err != nil || len(saved.Problems) > 0 {
		t.Fatalf("%+v %v", saved, err)
	}
	if got := names(t, saved.Content); got != "b@C0000BBB,c@C0000AAA,a@C0000BBB" {
		t.Errorf("order = %s", got)
	}
	f = ToForm(raw.Route("b"))
	f.OriginalName, f.Name = "", ""
	saved, _ = Save(data, raw, f)
	if saved.Name != "rule-1" || !strings.HasSuffix(names(t, saved.Content), "rule-1@C0000BBB") {
		t.Errorf("new rule = %s / %s", saved.Name, names(t, saved.Content))
	}
	if _, err := Save(data, raw, Form{OriginalName: "gone"}); err == nil {
		t.Error("changing a missing rule should fail")
	}
}
