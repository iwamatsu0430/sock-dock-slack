package gateway

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/handler"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

type post struct{ channel, thread, text string }

type fakeAPI struct {
	mu        sync.Mutex
	posts     []post
	reactions map[string]int // ts -> net count
}

func (f *fakeAPI) PostMessage(_ context.Context, channel, threadTS, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, post{channel, threadTS, text})
	return nil
}
func (f *fakeAPI) AddReaction(_ context.Context, _, ts, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions[ts]++
	return nil
}
func (f *fakeAPI) RemoveReaction(_ context.Context, _, ts, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactions[ts]--
	return nil
}
func (f *fakeAPI) Permalink(_ context.Context, channel, ts string) (string, error) {
	return "https://example.slack.com/archives/" + channel + "/p" + strings.ReplaceAll(ts, ".", ""), nil
}
func (f *fakeAPI) AuthTest(context.Context) (slackio.Identity, error) {
	return slackio.Identity{UserID: "UBOT", BotID: "BBOT", Team: "test"}, nil
}

func (f *fakeAPI) postsIn(thread string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, p := range f.posts {
		if p.thread == thread {
			out = append(out, p.text)
		}
	}
	return out
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

type harness struct {
	gw    *Gateway
	api   *fakeAPI
	db    *store.Store
	mu    sync.Mutex
	calls []handler.Input
	opts  []handler.ClaudeOptions
}

func newHarness(t *testing.T, claude func(handler.Input) (handler.Result, error)) *harness {
	t.Helper()
	dir := t.TempDir()
	cfg, err := config.Parse([]byte(fmt.Sprintf(`
settings:
  claude_command: /bin/echo
routes:
  - name: entry
    channel: C1
    match: '^entry\s+(?P<code>\w+)'
    handler: {type: claude, cwd: %q, timeout_sec: 5}
  - name: echo
    channel: C1
    match: '^echo\s+(?P<body>.+)'
    handler: {type: command, cwd: %q, command: /bin/echo}
`, dir, dir)), filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &harness{api: &fakeAPI{reactions: map[string]int{}}, db: db}
	h.gw = New(Options{
		Store: db, API: h.api, BotToken: "xoxb-test",
		RunClaude: func(_ context.Context, _ *config.Route, in handler.Input, o handler.ClaudeOptions, _ handler.Env) (handler.Result, error) {
			h.mu.Lock()
			h.calls = append(h.calls, in)
			h.opts = append(h.opts, o)
			h.mu.Unlock()
			return claude(in)
		},
		RunCommand: func(_ context.Context, _ *config.Route, in handler.Input, _ handler.Env) (handler.Result, error) {
			return handler.Result{Text: "echo: " + in.Groups["body"]}, nil
		},
	}, cfg)
	if err := h.gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return h
}

func msg(ts, threadTS, text string) event.Message {
	return event.Message{Channel: "C1", TS: ts, ThreadTS: threadTS, User: "U1", Text: text}
}

func TestThreadConversation(t *testing.T) {
	turn := 0
	h := newHarness(t, func(in handler.Input) (handler.Result, error) {
		turn++
		return handler.Result{Text: fmt.Sprintf("**reply %d** to %s", turn, in.Messages[len(in.Messages)-1].Text), SessionID: "sess-1"}, nil
	})

	h.gw.HandleMessage(msg("100.1", "", "entry AB12 please"))
	waitFor(t, "first reply", func() bool { return len(h.api.postsIn("100.1")) == 1 })
	if got := h.api.postsIn("100.1")[0]; got != "*reply 1* to entry AB12 please" {
		t.Fatalf("first reply = %q (mrkdwn conversion expected)", got)
	}

	h.gw.HandleMessage(msg("100.2", "100.1", "OK"))
	waitFor(t, "second reply", func() bool { return len(h.api.postsIn("100.1")) == 2 })

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) != 2 {
		t.Fatalf("calls = %d", len(h.calls))
	}
	first, second := h.calls[0], h.calls[1]
	if first.Turn != 1 || second.Turn != 2 {
		t.Errorf("turns = %d, %d", first.Turn, second.Turn)
	}
	if second.Groups["code"] != "AB12" {
		t.Errorf("groups not carried to later turns: %v", second.Groups)
	}
	if h.opts[0].SessionID != "" || h.opts[1].SessionID != "sess-1" {
		t.Errorf("session ids = %q, %q", h.opts[0].SessionID, h.opts[1].SessionID)
	}
	if !strings.Contains(first.Permalink, "/archives/C1/p1001") {
		t.Errorf("permalink = %q", first.Permalink)
	}
	for ts, n := range h.api.reactions {
		if n != 0 {
			t.Errorf("reaction on %s not removed (%d)", ts, n)
		}
	}
	turns, _ := h.db.ListTurns(context.Background(), 10)
	for _, tr := range turns {
		if tr.Status != store.StatusOK {
			t.Errorf("turn %d status %s", tr.ID, tr.Status)
		}
	}
}

func TestIgnoredMessages(t *testing.T) {
	h := newHarness(t, func(handler.Input) (handler.Result, error) { return handler.Result{Text: "x"}, nil })
	h.gw.HandleMessage(msg("1.1", "", "hello there"))                // no route
	h.gw.HandleMessage(msg("1.2", "9.9", "reply to unknown thread")) // unbound thread
	bot := msg("1.3", "", "entry 1234")
	bot.BotID = "B999"
	h.gw.HandleMessage(bot) // bot message
	self := msg("1.4", "", "entry 1234")
	self.User = "UBOT"
	h.gw.HandleMessage(self) // our own message
	edited := msg("1.5", "", "entry 1234")
	edited.SubType = "message_changed"
	h.gw.HandleMessage(edited)
	other := msg("1.6", "", "entry 1234")
	other.Channel = "C2"
	h.gw.HandleMessage(other) // other channel

	h.gw.HandleMessage(msg("2.1", "", "echo hi"))
	h.gw.HandleMessage(msg("2.1", "", "echo hi")) // duplicate delivery
	waitFor(t, "echo reply", func() bool { return len(h.api.postsIn("2.1")) == 1 })
	time.Sleep(50 * time.Millisecond)

	h.api.mu.Lock()
	defer h.api.mu.Unlock()
	if len(h.api.posts) != 1 || h.api.posts[0].text != "echo: hi" {
		t.Fatalf("posts = %+v", h.api.posts)
	}
}

func TestErrorsAndTimeout(t *testing.T) {
	h := newHarness(t, func(in handler.Input) (handler.Result, error) {
		if strings.Contains(in.Messages[0].Text, "slow") {
			return handler.Result{}, handler.ErrTimeout
		}
		return handler.Result{}, &handler.ExitError{Code: 1, Stderr: "trace"}
	})
	h.gw.HandleMessage(msg("3.1", "", "entry slow"))
	h.gw.HandleMessage(msg("4.1", "", "entry fail"))
	waitFor(t, "error replies", func() bool {
		return len(h.api.postsIn("3.1")) == 1 && len(h.api.postsIn("4.1")) == 1
	})
	if got := h.api.postsIn("3.1")[0]; !strings.Contains(got, "5 seconds") {
		t.Errorf("timeout message = %q", got)
	}
	if got := h.api.postsIn("4.1")[0]; !strings.Contains(got, "route: entry") {
		t.Errorf("error message = %q", got)
	}
	turns, _ := h.db.ListTurns(context.Background(), 10)
	statuses := map[string]bool{}
	for _, tr := range turns {
		statuses[tr.Status] = true
	}
	if !statuses[store.StatusTimeout] || !statuses[store.StatusError] {
		t.Errorf("statuses = %v", statuses)
	}
}

func TestPauseAndMissingRoute(t *testing.T) {
	h := newHarness(t, func(handler.Input) (handler.Result, error) { return handler.Result{Text: "ok"}, nil })
	ctx := context.Background()
	h.gw.HandleMessage(msg("5.1", "", "entry 1"))
	waitFor(t, "reply", func() bool { return len(h.api.postsIn("5.1")) == 1 })

	if err := h.gw.SetPaused(ctx, "entry", true); err != nil {
		t.Fatal(err)
	}
	h.gw.HandleMessage(msg("6.1", "", "entry 2")) // new thread on paused route: ignored
	h.gw.HandleMessage(msg("5.2", "5.1", "more")) // reply on paused route: notice
	waitFor(t, "paused notice", func() bool { return len(h.api.postsIn("5.1")) == 2 })
	if got := h.api.postsIn("5.1")[1]; !strings.Contains(got, "paused") {
		t.Errorf("notice = %q", got)
	}
	if len(h.api.postsIn("6.1")) != 0 {
		t.Error("paused route started a thread")
	}
	if err := h.gw.SetPaused(ctx, "nope", true); err == nil {
		t.Error("pausing an unknown route should fail")
	}

	// Thread bound to a route that no longer exists.
	_ = h.gw.SetPaused(ctx, "entry", false)
	_ = h.db.BindThread(ctx, store.Thread{Channel: "C1", ThreadTS: "7.1", Route: "removed"})
	h.gw.HandleMessage(msg("7.2", "7.1", "hello?"))
	waitFor(t, "missing route notice", func() bool { return len(h.api.postsIn("7.1")) == 1 })
	if got := h.api.postsIn("7.1")[0]; !strings.Contains(got, "removed") {
		t.Errorf("notice = %q", got)
	}
}

func TestRecoverInterruptedTurns(t *testing.T) {
	h := newHarness(t, func(handler.Input) (handler.Result, error) { return handler.Result{}, nil })
	ctx := context.Background()
	id, _ := h.db.StartTurn(ctx, store.Turn{Channel: "C1", ThreadTS: "8.1", MessageTS: []string{"8.2"}, Route: "entry"})
	if err := h.gw.Start(ctx); err != nil { // simulates the next process start
		t.Fatal(err)
	}
	if got := h.api.postsIn("8.1"); len(got) != 1 || !strings.Contains(got[0], "restarted") {
		t.Fatalf("posts = %v", got)
	}
	turns, _ := h.db.ListTurns(ctx, 10)
	for _, tr := range turns {
		if tr.ID == id && tr.Status != store.StatusInterrupted {
			t.Fatalf("status = %s", tr.Status)
		}
	}
}
