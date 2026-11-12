package control

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

type nopAPI struct{}

func (nopAPI) PostMessage(context.Context, string, string, string) error    { return nil }
func (nopAPI) AddReaction(context.Context, string, string, string) error    { return nil }
func (nopAPI) RemoveReaction(context.Context, string, string, string) error { return nil }
func (nopAPI) Permalink(context.Context, string, string) (string, error)    { return "", nil }
func (nopAPI) AuthTest(context.Context) (slackio.Identity, error) {
	return slackio.Identity{UserID: "UBOT", Team: "t"}, nil
}

func TestControlAPI(t *testing.T) {
	// Unix socket paths are limited to ~104 bytes; t.TempDir() can be too long on macOS.
	dir, err := os.MkdirTemp("/tmp", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfgPath := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("routes:\n  - name: echo\n    channel: C1\n    match: '^echo (?P<body>.+)'\n    handler: {type: command, cwd: %q, command: /bin/echo}\n", dir)
	os.WriteFile(cfgPath, []byte(content), 0o600)
	os.WriteFile(filepath.Join(dir, "sockdock.log"), []byte("a\nb\nc\n"), 0o600)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	defer db.Close()
	gw := gateway.New(gateway.Options{ConfigPath: cfgPath, Store: db, API: nopAPI{}, Version: "test"}, cfg)
	if err := gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "c.sock")
	srv := NewServer(gw, slog.Default(), filepath.Join(dir, "sockdock.log"))
	go srv.Serve(sock)
	defer srv.Shutdown(context.Background())
	c := NewClient(sock)
	ctx := context.Background()
	waitUp(t, c)

	st, err := c.Status(ctx)
	if err != nil || st.Version != "test" || len(st.Routes) != 1 {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if fi, _ := os.Stat(sock); fi.Mode().Perm() != 0o600 {
		t.Errorf("socket mode = %v", fi.Mode().Perm())
	}

	m, err := c.Match(ctx, MatchRequest{Channel: "C1", Text: "echo hi"})
	if err != nil || !m.Matched || m.Groups["body"] != "hi" {
		t.Fatalf("match = %+v, %v", m, err)
	}

	if err := c.SetPaused(ctx, "echo", true); err != nil {
		t.Fatal(err)
	}
	st, _ = c.Status(ctx)
	if !st.Routes[0].Paused {
		t.Error("route not paused")
	}

	// Invalid config is rejected and the file is untouched.
	err = c.PutConfig(ctx, "routes:\n  - name: broken\n")
	if err == nil || !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if b, _ := os.ReadFile(cfgPath); string(b) != content {
		t.Fatal("config file changed after a rejected PUT")
	}

	// Valid config is written and reloaded.
	newContent := strings.Replace(content, "name: echo", "name: echo2", 1)
	if err := c.PutConfig(ctx, newContent); err != nil {
		t.Fatal(err)
	}
	if gw.Config().Route("echo2") == nil {
		t.Fatal("config not reloaded")
	}

	lines, err := c.Logs(ctx, 2)
	if err != nil || strings.Join(lines, ",") != "b,c" {
		t.Fatalf("logs = %v, %v", lines, err)
	}

	got := make(chan gateway.Event, 4)
	ectx, cancel := context.WithCancel(ctx)
	defer cancel()
	go c.Events(ectx, func(e gateway.Event) { got <- e })
	select {
	case e := <-got:
		if e.Type != "status" {
			t.Fatalf("first event = %s", e.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no initial event")
	}
	_ = c.Reload(ctx)
	select {
	case e := <-got:
		if e.Type != "config_reloaded" {
			t.Fatalf("event = %s", e.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reload event")
	}
}

func TestClientNotRunning(t *testing.T) {
	c := NewClient("/tmp/definitely-not-a-sockdock.sock")
	if _, err := c.Status(context.Background()); err != ErrNotRunning {
		t.Fatalf("err = %v", err)
	}
}

func waitUp(t *testing.T, c *Client) {
	t.Helper()
	for range 100 {
		if _, err := c.Status(context.Background()); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("control server did not start")
}
