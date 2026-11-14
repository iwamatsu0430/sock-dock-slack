package control

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

type memberAPI struct {
	nopAPI
	err error
}

func (m memberAPI) Members(context.Context) ([]slackio.Member, error) {
	return []slackio.Member{{ID: "U01ALEX", Name: "alex", RealName: "Alex Morgan"}, {ID: "U02TARO", Name: "田中 太郎"}}, m.err
}

func startWith(t *testing.T, api slackio.API) (*Client, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "sd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	cfgPath := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("routes:\n  - name: only\n    channel: C0123ABCD\n    starts_with: review\n    allowed_users: [U01ALEX, U09GONE]\n    handler: {type: command, cwd: %q, command: /bin/echo}\n", dir)
	os.WriteFile(cfgPath, []byte(content), 0o600)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	db, _ := store.Open(filepath.Join(dir, "t.db"))
	t.Cleanup(func() { db.Close() })
	gw := gateway.New(gateway.Options{ConfigPath: cfgPath, Store: db, API: api, Version: "test"}, cfg)
	if err := gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "c.sock")
	srv := NewServer(gw, slog.Default(), filepath.Join(dir, "sockdock.log"))
	go srv.Serve(sock)
	t.Cleanup(func() { srv.Shutdown(context.Background()) })
	c := NewClient(sock)
	waitUp(t, c)
	return c, dir
}

func TestUsersAndAllowedUsers(t *testing.T) {
	ctx := context.Background()
	c, dir := startWith(t, memberAPI{})

	res, err := c.Users(ctx, "ALEX", 10)
	if err != nil || res.Problem != "" || len(res.Members) != 1 || res.Members[0].ID != "U01ALEX" {
		t.Fatalf("users = %+v, %v", res, err)
	}

	// The rule's people come back named, in order; unknown IDs keep only the ID.
	ri, err := c.Rule(ctx, "only")
	if err != nil || len(ri.Users) != 2 || ri.Users[0].Name != "alex" || ri.Users[1].ID != "U09GONE" || ri.Users[1].Name != "" {
		t.Fatalf("rule users = %+v, %v", ri.Users, err)
	}
	if !ri.Form.Restricted {
		t.Error("form should be restricted")
	}

	// Matching: someone else's post passes the rule over; without a user the restriction is reported.
	if m, _ := c.Match(ctx, MatchRequest{Channel: "C0123ABCD", Text: "review x", User: "U05SAM"}); m.Matched {
		t.Errorf("someone else matched: %+v", m)
	}
	if m, _ := c.Match(ctx, MatchRequest{Channel: "C0123ABCD", Text: "review x", User: "U01ALEX"}); !m.Matched {
		t.Errorf("alex did not match: %+v", m)
	}
	if m, _ := c.Match(ctx, MatchRequest{Channel: "C0123ABCD", Text: "review x"}); !m.Matched || len(m.AllowedUsers) != 2 {
		t.Errorf("without user: %+v", m)
	}

	// Saving an agent rule open to everyone with commands reports it.
	saved, err := c.SaveRule(ctx, rules.Form{Channel: "C0123ABCD", Trigger: "starts_with", Keyword: "x", Type: "claude", Folder: dir, Commands: []string{"npm test"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Problems) == 0 && saved.OpenToAll != "commands" {
		t.Errorf("open_to_all = %q (%+v)", saved.OpenToAll, saved)
	}
}

func TestUsersProblems(t *testing.T) {
	ctx := context.Background()
	c, _ := startWith(t, nopAPI{})
	if res, err := c.Users(ctx, "", 10); err != nil || res.Problem != "unavailable" {
		t.Errorf("no member lister: %+v, %v", res, err)
	}
	c, _ = startWith(t, memberAPI{err: errors.New("missing_scope")})
	if res, err := c.Users(ctx, "", 10); err != nil || res.Problem != "missing_scope" {
		t.Errorf("missing scope: %+v, %v", res, err)
	}
	// Without names the rule still lists its IDs.
	if ri, err := c.Rule(ctx, "only"); err != nil || len(ri.Users) != 2 || ri.Users[0].Name != "" {
		t.Errorf("rule users without users:read = %+v, %v", ri.Users, err)
	}
}
