package updates

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v0.2.0", "v0.1.1", true},
		{"v0.10.0", "v0.9.0", true},
		{"v1.0.0", "v0.99.99", true},
		{"v0.1.1", "v0.1.1", false},
		{"v0.1.0", "v0.1.1", false},
		{"0.2.0", "v0.1.1", true},
		// Builds that are not releases never compare.
		{"v0.2.0", "dev", false},
		{"v0.2.0", "v0.1.1-6-g7e68b09", false},
		{"v0.2.0", "0.0.0-6b86b26-dirty", false},
		{"v0.2.0-rc1", "v0.1.1", false},
	}
	for _, c := range cases {
		if got := Newer(c.latest, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v", c.latest, c.current, got)
		}
	}
	if IsRelease("v0.1.1-6-g7e68b09") || !IsRelease("v0.1.1") {
		t.Error("IsRelease is wrong")
	}
}

func TestNotes(t *testing.T) {
	body := "## English\r\n- Rules can react to DMs.\r\n- `mention_only`.\r\n\r\n## 日本語\r\n- DM でもルールを動かせます。\r\n"
	n := Notes(body)
	if n["en"] != "- Rules can react to DMs.\n- `mention_only`." {
		t.Errorf("en = %q", n["en"])
	}
	if n["ja"] != "- DM でもルールを動かせます。" {
		t.Errorf("ja = %q", n["ja"])
	}
	// Without sections, both get the whole body.
	plain := "**Full Changelog**: https://github.com/x/y/compare/v0.1.0...v0.1.1"
	if n := Notes(plain); n["en"] != plain || n["ja"] != plain {
		t.Errorf("no sections: %v", n)
	}
	// Only English written: Japanese gets the whole body.
	if got := Section("## English\n- a\n", "ja"); got != "## English\n- a" {
		t.Errorf("ja fallback = %q", got)
	}
	if Section("", "en") != "" {
		t.Error("empty body should give empty notes")
	}
}

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "SockDock/v0.1.1" {
			t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
		}
		w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://github.com/r/releases/tag/v0.2.0","body":"## English\n- x"}`))
	}))
	defer srv.Close()
	rel, err := Latest(context.Background(), srv.Client(), srv.URL, "SockDock/v0.1.1")
	if err != nil || rel.Version != "0.2.0" || rel.Tag != "v0.2.0" || rel.URL == "" || rel.Body == "" {
		t.Fatalf("release = %+v, %v", rel, err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer bad.Close()
	if _, err := Latest(context.Background(), bad.Client(), bad.URL, "x"); err == nil {
		t.Error("a non-200 answer should fail")
	}
}
