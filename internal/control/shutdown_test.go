package control

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// An open event stream (the menu bar app keeps one) must not hold up Shutdown.
func TestShutdownEndsEventStreams(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(fmt.Sprintf("routes:\n  - name: a\n    channel: C0123ABCD\n    all_posts: true\n    handler: {type: command, cwd: %q, command: /bin/echo}\n", dir)), 0o600)
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
	c := NewClient(sock)
	waitUp(t, c)

	streaming := make(chan struct{})
	go c.Events(context.Background(), func(gateway.Event) {
		select {
		case <-streaming:
		default:
			close(streaming)
		}
	})
	select {
	case <-streaming:
	case <-time.After(3 * time.Second):
		t.Fatal("no event stream")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("shutdown took %v with an event stream open", d)
	}
}
