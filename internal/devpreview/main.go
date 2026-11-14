// Command devpreview runs a gateway against a fake Slack, for checking the
// app's screens in a browser without a real workspace or the real config
// (scripts/preview.sh starts it together with the GUI in server mode).
//
// It holds the data directory's lock, so the GUI only shows this gateway
// instead of starting its own. Sending a DM from the fake person 田中 太郎
// (for example an owner code) is a GET request:
//
//	curl 'http://127.0.0.1:8766/dm?text=123456'
//
// Environment: SOCKDOCK_DATA_DIR (required), SOCKDOCK_CONFIG (default
// <data>/config.yaml, written with sample rules when missing),
// PREVIEW_NO_IM=1 (the Slack app lacks im:history), PREVIEW_DM_ADDR
// (default 127.0.0.1:8766).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/lockfile"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// fakeSlack answers like a workspace "acme" with two channels and two people.
// Posts are only logged.
type fakeSlack struct {
	log    *slog.Logger
	scopes []string
}

func (f fakeSlack) PostMessage(_ context.Context, channel, threadTS, text string) error {
	f.log.Info("post", "channel", channel, "thread_ts", threadTS, "text", text)
	return nil
}
func (fakeSlack) AddReaction(context.Context, string, string, string) error    { return nil }
func (fakeSlack) RemoveReaction(context.Context, string, string, string) error { return nil }
func (fakeSlack) Permalink(_ context.Context, channel, ts string) (string, error) {
	return "https://acme.slack.com/archives/" + channel + "/p" + ts, nil
}
func (fakeSlack) AuthTest(context.Context) (slackio.Identity, error) {
	return slackio.Identity{UserID: "UBOT", BotID: "BBOT", Team: "acme"}, nil
}
func (fakeSlack) Members(context.Context) ([]slackio.Member, error) {
	return []slackio.Member{{ID: "U02TARO", Name: "田中 太郎", RealName: "Taro Tanaka"}, {ID: "U01ALEX", Name: "alex"}}, nil
}
func (fakeSlack) Channels(context.Context) ([]slackio.Channel, error) {
	return []slackio.Channel{{ID: "C0123DEV", Name: "dev-help"}, {ID: "C0123GEN", Name: "general"}}, nil
}
func (fakeSlack) ChannelInfo(_ context.Context, id string) (slackio.Channel, error) {
	return slackio.Channel{ID: id, Name: "dev-help"}, nil
}
func (f fakeSlack) Scopes(context.Context) ([]string, error) { return f.scopes, nil }

const sampleConfig = `settings:
  language: auto
routes:
  - name: rule-1
    channel: C0123DEV
    starts_with: review
    mention_only: true
    handler: {type: command, cwd: %[1]q, command: /bin/echo}
  - name: rule-2
    channel: C0123DEV
    starts_with: deploy
    handler: {type: command, cwd: %[1]q, command: /bin/echo}
  - name: rule-3
    channel: dm
    starts_with: todo
    allowed_users: [U02TARO]
    handler: {type: command, cwd: %[1]q, command: /bin/echo}
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "devpreview:", err)
		os.Exit(1)
	}
}

func run() error {
	if os.Getenv("SOCKDOCK_DATA_DIR") == "" {
		return fmt.Errorf("set SOCKDOCK_DATA_DIR to a throwaway directory")
	}
	dirs := paths.Default()
	if err := dirs.Ensure(); err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	lock, err := lockfile.Acquire(dirs.LockPath())
	if err != nil {
		return fmt.Errorf("another SockDock uses %s: %w", dirs.DataDir, err)
	}
	defer lock.Release()

	if _, err := os.Stat(dirs.ConfigFile); err != nil {
		work := filepath.Join(dirs.DataDir, "work")
		if err := os.MkdirAll(work, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(dirs.ConfigFile, []byte(fmt.Sprintf(sampleConfig, work)), 0o600); err != nil {
			return err
		}
	}
	cfg, err := config.LoadLenient(dirs.ConfigFile)
	if err != nil {
		return err
	}
	db, err := store.Open(dirs.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()

	scopes := append(append([]string{}, slackio.RequiredBotScopes...), slackio.OptionalBotScopes...)
	if os.Getenv("PREVIEW_NO_IM") != "" {
		scopes = scopes[:len(scopes)-1] // OptionalBotScopes ends with im:history
	}
	gw := gateway.New(gateway.Options{
		ConfigPath: dirs.ConfigFile, Store: db, API: fakeSlack{log: log, scopes: scopes}, Log: log, Version: "preview",
	}, cfg)
	if err := gw.Start(context.Background()); err != nil {
		return err
	}
	gw.OnConnState(slackio.ConnState{Connected: true})

	srv := control.NewServer(gw, log, dirs.LogFile())
	go func() {
		if err := srv.Serve(dirs.SocketPath()); err != nil {
			log.Error("control API stopped", "err", err)
		}
	}()

	addr := os.Getenv("PREVIEW_DM_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8766"
	}
	var n atomic.Int64
	go func() {
		_ = http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ts := fmt.Sprintf("%d.%06d", time.Now().Unix(), n.Add(1))
			gw.HandleMessage(event.Message{Channel: "D0TARO", ChannelType: "im", TS: ts, User: "U02TARO", Text: r.URL.Query().Get("text")})
			fmt.Fprintln(w, "sent")
		}))
	}()
	log.Info("preview gateway ready", "data_dir", dirs.DataDir, "dm", "http://"+addr+"/dm?text=…")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	return gw.Shutdown(ctx)
}
