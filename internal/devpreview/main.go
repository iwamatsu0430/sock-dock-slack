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
// Add file=<name> to attach a file to the DM.
//
// Environment: SOCKDOCK_DATA_DIR (required), SOCKDOCK_CONFIG (default
// <data>/config.yaml, written with sample rules when missing),
// PREVIEW_NO_IM=1 (the Slack app lacks im:history), PREVIEW_NO_FILES=1 (it
// lacks files:read), PREVIEW_NO_UPDATE=1 (no newer version), PREVIEW_DM_ADDR
// (default 127.0.0.1:8766).
//
// The preview runs as v0.1.1 and a fake GitHub serves v0.2.0, so the update
// dialog and the setting can be checked (open the dialog from the page with
// wails.Events.Emit("sockdock:navigate", ":update") or the app's menu).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	"github.com/iwamatsu0430/sock-dock-slack/internal/updates"
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
func (f fakeSlack) FileInfo(context.Context, string) (event.File, error) {
	return event.File{}, errors.New("not in the preview")
}

// DownloadFile writes a short text, or fails like Slack without files:read.
func (f fakeSlack) DownloadFile(_ context.Context, _ string, w io.Writer) error {
	if len(slackio.MissingScopes(f.scopes, []string{slackio.ScopeFilesRead})) > 0 {
		return slackio.ErrNoFileAccess
	}
	_, err := io.WriteString(w, "preview file")
	return err
}

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

	var scopes []string
	for _, sc := range append(append([]string{}, slackio.RequiredBotScopes...), slackio.OptionalBotScopes...) {
		if sc == slackio.ScopeIMHistory && os.Getenv("PREVIEW_NO_IM") != "" ||
			sc == slackio.ScopeFilesRead && os.Getenv("PREVIEW_NO_FILES") != "" {
			continue
		}
		scopes = append(scopes, sc)
	}
	version, updateURL := "preview", ""
	if os.Getenv("PREVIEW_NO_UPDATE") == "" {
		version = "v0.1.1"
		if updateURL, err = fakeGitHub(db); err != nil {
			return err
		}
	}
	gw := gateway.New(gateway.Options{
		ConfigPath: dirs.ConfigFile, Store: db, API: fakeSlack{log: log, scopes: scopes}, Log: log, Version: version,
		FilesDir: dirs.FilesDir, UpdateURL: updateURL,
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
			m := event.Message{Channel: "D0TARO", ChannelType: "im", TS: ts, User: "U02TARO", Text: r.URL.Query().Get("text")}
			if name := r.URL.Query().Get("file"); name != "" {
				m.SubType = "file_share"
				m.Files = []event.File{{ID: "F" + ts, Name: name, Mimetype: "text/plain", Size: 12, Mode: "hosted", URL: "https://files.slack.com/preview/" + name}}
			}
			gw.HandleMessage(m)
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

// fakeRelease is the newer version the preview offers.
var fakeRelease = updates.Release{
	Tag: "v0.2.0", Version: "0.2.0", URL: "https://github.com/iwamatsu0430/sock-dock-slack/releases",
	Body: "## English\n- Rules can react to DMs to the Slack app.\n- **Only when mentioned** for channel rules.\n- Files attached to a post reach the rule (`files:read`).\n\n" +
		"## 日本語\n- Slack アプリへの DM でもルールを動かせるようになりました。\n- チャンネルのルールに「**メンションされたときだけ**」を足しました。\n- 投稿に添付したファイルを、ルールに渡せるようになりました（`files:read`）。",
}

// fakeGitHub serves fakeRelease as GitHub's latest release and keeps it as
// already found, so the update shows at once (the gateway's state key
// "update_latest", internal/gateway/update.go).
func fakeGitHub(db *store.Store) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	go func() {
		_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": fakeRelease.Tag, "html_url": fakeRelease.URL, "body": fakeRelease.Body})
		}))
	}()
	if err := db.SetState(context.Background(), "update_latest", fakeRelease); err != nil {
		return "", err
	}
	return "http://" + ln.Addr().String(), nil
}
