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
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// reactAPI is fakeAPI plus the posts and reactions a reaction rule reads. Like
// Slack, it sends SockDock's own 👀 back as reaction events.
type reactAPI struct {
	*fakeAPI
	gw *Gateway

	rmu   sync.Mutex
	msgs  map[string]event.Message       // top-level posts by ts
	users map[string]map[string][]string // ts -> emoji -> who has it
}

func (a *reactAPI) Post(_ context.Context, channel, ts string) (event.Message, bool, error) {
	a.rmu.Lock()
	defer a.rmu.Unlock()
	m, ok := a.msgs[ts]
	m.Channel, m.TS = channel, ts
	return m, ok, nil
}

func (a *reactAPI) ReactionUsers(_ context.Context, _, ts string) (map[string][]string, error) {
	a.rmu.Lock()
	defer a.rmu.Unlock()
	return a.users[ts], nil
}

func (a *reactAPI) AddReaction(ctx context.Context, channel, ts, name string) error {
	err := a.fakeAPI.AddReaction(ctx, channel, ts, name)
	a.gw.HandleReaction(event.ReactionEvent{Channel: channel, TS: ts, Reaction: event.Reaction{Name: name, User: "UBOT"}})
	return err
}

func (a *reactAPI) RemoveReaction(ctx context.Context, channel, ts, name string) error {
	err := a.fakeAPI.RemoveReaction(ctx, channel, ts, name)
	a.gw.HandleReaction(event.ReactionEvent{Channel: channel, TS: ts, Reaction: event.Reaction{Name: name, User: "UBOT", Removed: true}})
	return err
}

// post puts a top-level post where the fake Slack can find it.
func (a *reactAPI) post(ts string, m event.Message) {
	a.rmu.Lock()
	defer a.rmu.Unlock()
	a.msgs[ts] = m
}

// has sets who has the emoji on a post.
func (a *reactAPI) has(ts, name string, users ...string) {
	a.rmu.Lock()
	defer a.rmu.Unlock()
	a.users[ts] = map[string][]string{name: users}
}

type reactHarness struct {
	gw  *Gateway
	api *reactAPI
	db  *store.Store

	mu    sync.Mutex
	calls []handler.Input
	fail  error
}

func (h *reactHarness) inputs() []handler.Input {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]handler.Input{}, h.calls...)
}

// settle gives events that should do nothing the time to do something.
func (h *reactHarness) settle(t *testing.T) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	waitTurnsFinished(t, h.db)
}

func (h *reactHarness) runs(t *testing.T, n int) []handler.Input {
	t.Helper()
	waitFor(t, fmt.Sprintf("%d runs", n), func() bool { return len(h.inputs()) >= n })
	h.settle(t)
	got := h.inputs()
	if len(got) != n {
		t.Fatalf("runs = %d, want %d", len(got), n)
	}
	return got
}

// newReactHarness starts a gateway whose rules are the given YAML lines (each
// rule without its handler, which is a program).
func newReactHarness(t *testing.T, rules ...string) *reactHarness {
	t.Helper()
	dir := t.TempDir()
	var b strings.Builder
	b.WriteString("settings:\n  language: en\nroutes:\n")
	for i, r := range rules {
		fmt.Fprintf(&b, "  - name: r%d\n    channel: C1\n    %s\n    handler: {type: command, cwd: %q, command: /bin/echo}\n", i+1, r, dir)
	}
	cfg, err := config.Parse([]byte(b.String()), filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &reactHarness{db: db, api: &reactAPI{
		fakeAPI: &fakeAPI{reactions: map[string]int{}},
		msgs:    map[string]event.Message{}, users: map[string]map[string][]string{},
	}}
	h.gw = New(Options{
		Store: db, API: h.api,
		RunCommand: func(_ context.Context, _ *config.Route, in handler.Input, _ handler.Env) (handler.Result, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.calls = append(h.calls, in)
			if h.fail != nil {
				return handler.Result{}, h.fail
			}
			return handler.Result{Text: "done"}, nil
		},
	}, cfg)
	h.api.gw = h.gw
	if err := h.gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return h
}

func reaction(ts, name, user string, removed bool) event.ReactionEvent {
	return event.ReactionEvent{Channel: "C1", TS: ts, Reaction: event.Reaction{Name: name, User: user, Removed: removed}}
}

// Someone adds the rule's reaction to another app's post: the rule runs once
// on that post, and the thread goes on like any other.
func TestReactionStartsARule(t *testing.T) {
	h := newReactHarness(t, "reaction: mag")
	h.api.post("100.1", event.Message{BotID: "BALERT", SubType: "bot_message", Text: "api-server: 5xx above 5%"})

	h.gw.HandleReaction(reaction("100.1", "mag::skin-tone-2", "U01", false))
	// A second person, and the same person again, do nothing.
	h.gw.HandleReaction(reaction("100.1", "mag", "U02", false))
	h.gw.HandleReaction(reaction("100.1", "mag", "U01", false))
	// Another emoji, and removing the reaction (the rule has no
	// reaction_removed), do nothing.
	h.gw.HandleReaction(reaction("100.1", "eyes", "U01", false))
	h.gw.HandleReaction(reaction("100.1", "mag", "U01", true))

	in := h.runs(t, 1)[0]
	if in.Route != "r1" || in.Turn != 1 || in.ThreadTS != "100.1" || in.Groups["text"] != "api-server: 5xx above 5%" {
		t.Errorf("input = %+v", in)
	}
	if r := in.Reaction; r == nil || r.Name != "mag" || r.User != "U01" || r.Action != config.ReactionAdded {
		t.Errorf("reaction = %+v", in.Reaction)
	}
	if len(in.Messages) != 1 || in.Messages[0].Text != "api-server: 5xx above 5%" || in.Messages[0].TS != "100.1" {
		t.Errorf("messages = %+v", in.Messages)
	}
	if got := h.api.postsIn("100.1"); len(got) != 1 || got[0] != "done" {
		t.Errorf("replies = %v", got)
	}
	if n := h.api.reactionCount("100.1"); n != 0 {
		t.Errorf("👀 left on the post: %d", n)
	}

	// A reply in the thread continues with the same rule, without a reaction.
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "100.2", ThreadTS: "100.1", User: "U03", Text: "will a rollback fix it?"})
	second := h.runs(t, 2)[1]
	if second.Turn != 2 || second.Reaction != nil || second.Messages[0].Text != "will a rollback fix it?" {
		t.Errorf("second run = %+v", second)
	}
	if got := h.gw.Status(0).HeardReactions; len(got) != 1 || got[0] != "C1" {
		t.Errorf("heard reactions = %v", got)
	}
}

// The rule's emoji is the 👀 SockDock itself adds while it runs. Its own
// reactions and posts must never start a run, or one run would start the next.
func TestOwnReactionsNeverStartARun(t *testing.T) {
	h := newReactHarness(t, "reaction: eyes\n    reaction_removed: true")
	h.api.post("100.1", event.Message{User: "U09", Text: "staging login is broken"})
	h.api.post("200.1", event.Message{User: "U09", Text: "another post"})
	h.api.post("300.1", event.Message{BotID: "BBOT", User: "UBOT", Text: "a notice from SockDock"})

	// SockDock's own 👀 on a post no rule ran for.
	h.gw.HandleReaction(reaction("200.1", "eyes", "UBOT", false))
	// A person's 👀 on SockDock's own post, with and without Slack naming the author.
	h.gw.HandleReaction(reaction("300.1", "eyes", "U01", false))
	ev := reaction("300.1", "eyes", "U01", false)
	ev.ItemUser = "UBOT"
	h.gw.HandleReaction(ev)
	h.settle(t)
	if n := len(h.inputs()); n != 0 {
		t.Fatalf("runs = %d, want none", n)
	}

	// A person's 👀 starts one run. While it runs SockDock adds and removes
	// its own 👀 on the same post, and Slack sends both back.
	h.gw.HandleReaction(reaction("100.1", "eyes", "U01", false))
	h.runs(t, 1)

	// The person takes their 👀 off: the rule runs once more ("removed"), and
	// SockDock's own 👀 around that run again starts nothing.
	h.gw.HandleReaction(reaction("100.1", "eyes", "U01", true))
	got := h.runs(t, 2)
	if got[1].Reaction == nil || got[1].Reaction.Action != config.ReactionRemoved {
		t.Errorf("second run = %+v", got[1].Reaction)
	}
	if n := h.api.reactionCount("100.1"); n != 0 {
		t.Errorf("👀 left on the post: %d", n)
	}
}

func TestReactionsThatDoNothing(t *testing.T) {
	h := newReactHarness(t, "starts_with: review", "reaction: mag\n    allowed_users: [U01]")
	h.api.post("100.1", event.Message{User: "U09", Text: "review this please"})
	h.api.post("200.1", event.Message{User: "U09", Text: "an alert"})

	// A post a rule already ran for.
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "100.1", User: "U09", Text: "review this please"})
	h.runs(t, 1)
	h.gw.HandleReaction(reaction("100.1", "mag", "U01", false))
	// A reply in a thread: the fake Slack has no top-level post with that ts.
	h.gw.HandleReaction(reaction("100.5", "mag", "U01", false))
	// Someone who may not run the rule.
	h.gw.HandleReaction(reaction("200.1", "mag", "U02", false))
	h.settle(t)
	if n := len(h.inputs()); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}

	// The person who may run it.
	h.gw.HandleReaction(reaction("200.1", "mag", "U01", false))
	if in := h.runs(t, 2)[1]; in.Route != "r2" || in.Reaction.User != "U01" {
		t.Errorf("run = %+v", in)
	}
}

// With reaction_removed the rule runs again when nobody who may run it has
// the reaction on the post any more, and again when it comes back.
func TestReactionRemoved(t *testing.T) {
	h := newReactHarness(t, "reaction: ticket\n    reaction_removed: true\n    allowed_users: [U01, U02]")
	h.api.post("100.1", event.Message{User: "U09", Text: "search shows 0 results"})

	// Removing it from a post the rule never ran for does nothing.
	h.gw.HandleReaction(reaction("100.1", "ticket", "U01", true))
	h.settle(t)

	h.gw.HandleReaction(reaction("100.1", "ticket", "U01", false))
	h.runs(t, 1)

	// U01 takes it off, but U02 (who may run the rule) still has it.
	h.api.has("100.1", "ticket", "U02", "U03")
	h.gw.HandleReaction(reaction("100.1", "ticket", "U01", true))
	// U03 may not run the rule: their removal is not looked at.
	h.api.has("100.1", "ticket", "U02")
	h.gw.HandleReaction(reaction("100.1", "ticket", "U03", true))
	h.settle(t)
	if n := len(h.inputs()); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}

	// U02 takes it off. U03's reaction is still there, but does not count;
	// Slack may still list U02's.
	h.api.has("100.1", "ticket", "U02", "U03", "UBOT")
	h.gw.HandleReaction(reaction("100.1", "ticket", "U02", true))
	got := h.runs(t, 2)[1]
	if got.Turn != 2 || got.ThreadTS != "100.1" || got.Messages[0].Text != "search shows 0 results" {
		t.Errorf("removed run = %+v", got)
	}
	if r := got.Reaction; r == nil || r.Action != config.ReactionRemoved || r.User != "U02" || r.Name != "ticket" {
		t.Errorf("removed reaction = %+v", got.Reaction)
	}
	// A second removal changes nothing.
	h.gw.HandleReaction(reaction("100.1", "ticket", "U01", true))
	h.settle(t)

	// Adding it back runs the rule as "added" again, in the same thread.
	h.gw.HandleReaction(reaction("100.1", "ticket", "U02", false))
	third := h.runs(t, 3)[2]
	if third.Turn != 3 || third.Reaction.Action != config.ReactionAdded || third.Reaction.User != "U02" {
		t.Errorf("third run = %+v", third)
	}
	if got := h.api.postsIn("100.1"); len(got) != 3 {
		t.Errorf("replies = %v", got)
	}
}

// A run started by a reaction does not ask the reader to send the post again.
func TestReactionTimeoutMessage(t *testing.T) {
	h := newReactHarness(t, "reaction: mag")
	h.fail = handler.ErrTimeout
	h.api.post("100.1", event.Message{User: "U09", Text: "an alert"})
	h.gw.HandleReaction(reaction("100.1", "mag", "U01", false))
	h.runs(t, 1)
	got := h.api.postsIn("100.1")
	if len(got) != 1 || !strings.Contains(got[0], "time limit") || strings.Contains(got[0], "smaller parts") {
		t.Errorf("replies = %v", got)
	}
	turns, err := h.db.ListTurns(context.Background(), 1)
	if err != nil || len(turns) != 1 || turns[0].Reaction != config.ReactionAdded || turns[0].Status != store.StatusTimeout {
		t.Errorf("turn = %+v, %v", turns, err)
	}
}
