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
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

func TestRuleEndpoints(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfgPath := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("routes:\n  - name: all\n    channel: C0123ABCD\n    all_posts: true\n    handler: {type: command, cwd: %q, command: /bin/echo}\n", dir)
	os.WriteFile(cfgPath, []byte(content), 0o600)
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
	c.Source = "mcp"
	ctx := context.Background()
	waitUp(t, c)

	events, cancel := gw.Subscribe()
	defer cancel()

	// A form with a problem is not saved.
	res, err := c.SaveRule(ctx, rules.Form{Channel: "C0123ABCD", Trigger: "starts_with", Type: "command", Folder: dir, Program: "/bin/echo"})
	if err != nil || res.OK || len(res.Problems) != 1 || res.Problems[0].Field != "keyword" {
		t.Fatalf("save without keyword = %+v, %v", res, err)
	}

	res, err = c.SaveRule(ctx, rules.Form{Channel: "C0123ABCD", Trigger: "starts_with", Keyword: "review", Type: "command", Folder: dir, Program: "/bin/echo"})
	if err != nil || !res.OK || res.Name != "rule-1" {
		t.Fatalf("save = %+v, %v", res, err)
	}
	if e := waitEvent(t, events, "rule_changed"); !strings.Contains(fmt.Sprint(e.Data), "added") {
		t.Errorf("event = %+v", e)
	}

	list, err := c.Rules(ctx)
	if err != nil || len(list) != 2 || list[1].Form.Name != "rule-1" || list[1].ShadowedBy == nil {
		t.Fatalf("rules = %+v, %v", list, err)
	}

	if _, err := c.OrderRules(ctx, OrderRequest{Channel: "C0123ABCD", Names: []string{"rule-1", "all"}}); err != nil {
		t.Fatal(err)
	}
	list, _ = c.Rules(ctx)
	if list[0].Form.Name != "rule-1" || list[0].ShadowedBy != nil || list[1].ShadowedBy != nil {
		t.Errorf("after reorder = %+v", list)
	}
	if r, _ := gw.Config().Match("C0123ABCD", "review this"); r == nil || r.Name != "rule-1" {
		t.Errorf("the gateway should use the new order, got %v", r)
	}
	if _, err := c.OrderRules(ctx, OrderRequest{Channel: "C0123ABCD", Names: []string{"rule-1"}}); err == nil {
		t.Error("an incomplete order should fail")
	}

	if res, err := c.DeleteRule(ctx, "rule-1"); err != nil || !res.OK {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	if _, err := c.Rule(ctx, "rule-1"); err == nil {
		t.Error("deleted rule still there")
	}
}

func waitEvent(t *testing.T, ch <-chan gateway.Event, typ string) gateway.Event {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type == typ {
				return e
			}
		case <-timeout:
			t.Fatalf("no %s event", typ)
		}
	}
}
