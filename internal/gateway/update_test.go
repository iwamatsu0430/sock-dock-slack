package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

func newUpdateGateway(t *testing.T, version, settings string, gh http.HandlerFunc) (*Gateway, *fakeAPI, *time.Time, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("settings:\n  language: en\n"+settings+"\nroutes: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		gh(w, r)
	}))
	t.Cleanup(srv.Close)
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	api := &fakeAPI{reactions: map[string]int{}}
	g := New(Options{
		ConfigPath: path, Store: db, API: api, Version: version,
		UpdateURL: srv.URL, UpdateClient: srv.Client(), Now: func() time.Time { return now },
	}, cfg)
	if err := g.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return g, api, &now, &calls
}

func release(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://github.com/x/releases/tag/` + tag + `","body":"## English\n- New things.\n## 日本語\n- 新しいこと。"}`))
	}
}

func waitUpdate(g *Gateway) {
	for range 200 {
		g.mu.Lock()
		busy := g.updateChecking
		g.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestUpdateCheckAndNotifyOnce(t *testing.T) {
	g, api, now, calls := newUpdateGateway(t, "v0.1.1", "  owner: UALEX\n  notify_dm: true", release("v0.2.0"))
	ev, cancel := g.Subscribe()
	defer cancel()
	ctx := context.Background()

	// Not before a minute after starting.
	g.checkUpdates(ctx)
	if calls.Load() != 0 {
		t.Fatal("checked too early")
	}
	*now = now.Add(updateFirstCheck)
	g.checkUpdates(ctx)
	waitUpdate(g)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
	st := g.Status(0)
	if st.Update == nil || st.Update.Version != "0.2.0" || st.Update.Notes["ja"] != "- 新しいこと。" || st.Update.Notes["en"] != "- New things." {
		t.Fatalf("update = %+v", st.Update)
	}
	posts := api.postsIn("")
	if len(posts) != 1 || posts[0] != "🆕 SockDock 0.2.0 is out. <https://github.com/x/releases/tag/v0.2.0|Release notes>" {
		t.Fatalf("DM = %q", posts)
	}
	gotEvent := false
	for len(ev) > 0 {
		if e := <-ev; e.Type == "update_available" {
			gotEvent = true
		}
	}
	if !gotEvent {
		t.Error("the app should be told")
	}

	// Within a day: no new check. After a day: a check, but no second notice.
	g.checkUpdates(ctx)
	if calls.Load() != 1 {
		t.Error("checked again within a day")
	}
	*now = now.Add(updateInterval)
	g.checkUpdates(ctx)
	waitUpdate(g)
	if calls.Load() != 2 || len(api.postsIn("")) != 1 {
		t.Errorf("calls = %d, DMs = %d", calls.Load(), len(api.postsIn("")))
	}

	// Turning the check off hides the newer version.
	if err := g.SetSettingBool("check_updates", false); err != nil {
		t.Fatal(err)
	}
	if g.Status(0).Update != nil || g.Status(0).CheckUpdates {
		t.Error("with the check off, no update is shown")
	}
}

func TestUpdateNotForLocalBuilds(t *testing.T) {
	for _, v := range []string{"dev", "v0.1.1-6-g7e68b09"} {
		g, _, now, calls := newUpdateGateway(t, v, "", release("v0.2.0"))
		*now = now.Add(updateFirstCheck)
		g.checkUpdates(context.Background())
		waitUpdate(g)
		if calls.Load() != 0 {
			t.Errorf("%s: a local build should not check", v)
		}
	}
}

func TestUpdateRetryAndSameVersion(t *testing.T) {
	fail := true
	g, api, now, calls := newUpdateGateway(t, "v0.2.0", "  owner: UALEX\n  notify_dm: true", func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		release("v0.2.0")(w, r)
	})
	ctx := context.Background()
	*now = now.Add(updateFirstCheck)
	g.checkUpdates(ctx)
	waitUpdate(g)
	// A failure is retried after an hour, and shows nothing.
	*now = now.Add(30 * time.Minute)
	g.checkUpdates(ctx)
	fail = false
	*now = now.Add(31 * time.Minute)
	g.checkUpdates(ctx)
	waitUpdate(g)
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want a retry after an hour", calls.Load())
	}
	// The same version is not newer.
	if g.Status(0).Update != nil || len(api.postsIn("")) != 0 {
		t.Errorf("same version: update = %+v, DMs = %q", g.Status(0).Update, strings.Join(api.postsIn(""), "|"))
	}
}
