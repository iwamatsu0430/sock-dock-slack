package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// ownerHarness runs a gateway whose config is a real file, so the owner can
// be written to it.
type ownerHarness struct {
	gw   *Gateway
	api  *fakeAPI
	path string
	mu   sync.Mutex
	now  time.Time
	runs []handler.Input
	fail error
}

func newOwnerHarness(t *testing.T, settings string) *ownerHarness {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf(`settings:
  language: en
  claude_command: /bin/echo
%s
routes:
  - name: todo
    channel: dm
    starts_with: todo
    handler: {type: claude, cwd: %q, timeout_sec: 5}
  - name: review
    channel: C1
    starts_with: review
    mention_only: true
    handler: {type: claude, cwd: %q, timeout_sec: 5}
`, settings, dir, dir)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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
	h := &ownerHarness{api: &fakeAPI{reactions: map[string]int{}}, path: path, now: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)}
	h.gw = New(Options{
		ConfigPath: path, Store: db, API: h.api,
		Now: func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now },
		RunClaude: func(_ context.Context, _ *config.Route, in handler.Input, _ handler.ClaudeOptions, _ handler.Env) (handler.Result, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.runs = append(h.runs, in)
			if h.fail != nil {
				return handler.Result{}, h.fail
			}
			return handler.Result{Text: "done"}, nil
		},
		ClaudeSignedIn: func(context.Context, []string, string) (bool, bool) { return true, true },
	}, cfg)
	if err := h.gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *ownerHarness) advance(d time.Duration) {
	h.mu.Lock()
	h.now = h.now.Add(d)
	h.mu.Unlock()
}

func (h *ownerHarness) runCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.runs)
}

func dm(ts, text string) event.Message {
	return event.Message{Channel: "D0ALEX", ChannelType: "im", TS: ts, User: "UALEX", Text: text}
}

func TestDMRuleRunsInThread(t *testing.T) {
	h := newOwnerHarness(t, "")
	h.gw.HandleMessage(dm("100.1", "todo milk"))
	waitFor(t, "reply in the DM thread", func() bool { return len(h.api.postsIn("100.1")) == 1 })
	if got := h.gw.Status(0).Heard; len(got) != 1 || got[0] != "dm" {
		t.Errorf("heard = %v, want [dm]", got)
	}
	// A reply in that thread continues the same rule.
	h.gw.HandleMessage(event.Message{Channel: "D0ALEX", ChannelType: "im", TS: "100.2", ThreadTS: "100.1", User: "UALEX", Text: "and eggs"})
	waitFor(t, "second reply", func() bool { return len(h.api.postsIn("100.1")) == 2 })
}

func TestMentionOnlyRule(t *testing.T) {
	h := newOwnerHarness(t, "")
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "200.1", User: "UALEX", Text: "review src/api.ts"})
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "200.2", User: "UALEX", Text: "<@UBOT> review src/api.ts"})
	waitFor(t, "reply to the mention", func() bool { return len(h.api.postsIn("200.2")) == 1 })
	if len(h.api.postsIn("200.1")) != 0 || h.runCount() != 1 {
		t.Errorf("a post without a mention should do nothing (runs %d)", h.runCount())
	}
	// A mention in a thread that no rule started does nothing.
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "201.2", ThreadTS: "201.1", User: "UALEX", Text: "<@UBOT> review src/x.ts"})
	time.Sleep(50 * time.Millisecond)
	if h.runCount() != 1 {
		t.Error("a mention in the middle of a thread should not start a rule")
	}
}

func TestRegisterOwnerWithCode(t *testing.T) {
	h := newOwnerHarness(t, "")
	ev, cancel := h.gw.Subscribe()
	defer cancel()

	// Without a registration in progress, six digits are an ordinary DM.
	h.gw.HandleMessage(dm("300.1", "123456"))
	if p := h.api.postsIn(""); len(p) != 0 {
		t.Fatalf("no reply expected, got %v", p)
	}

	reg, err := h.gw.StartRegistration()
	if err != nil || !reg.Active || len(reg.Code) != 6 {
		t.Fatalf("registration = %+v, %v", reg, err)
	}
	// A wrong code gets a reply and keeps the registration open.
	wrong := "000000"
	if reg.Code == wrong {
		wrong = "111111"
	}
	h.gw.HandleMessage(dm("300.2", wrong))
	if p := h.api.postsIn(""); len(p) != 1 || p[0] != config.DefaultMessages.WrongCode {
		t.Fatalf("posts = %v", p)
	}
	// Other DMs still go to the DM rules.
	h.gw.HandleMessage(dm("300.3", "todo milk"))
	waitFor(t, "rule reply", func() bool { return len(h.api.postsIn("300.3")) == 1 })

	// The previous code still works after it changes (typing takes time).
	h.advance(30 * time.Second)
	h.gw.HandleMessage(dm("300.4", reg.Code[:3]+" "+reg.Code[3:]))
	posts := h.api.postsIn("")
	if posts[len(posts)-1] != config.DefaultMessages.Registered {
		t.Fatalf("posts = %v", posts)
	}
	st := h.gw.Status(0)
	if st.Owner != "UALEX" || !st.NotifyDM {
		t.Errorf("owner not registered: %+v", st)
	}
	data, _ := os.ReadFile(h.path)
	if !strings.Contains(string(data), "owner: UALEX") || !strings.Contains(string(data), "notify_dm: true") {
		t.Errorf("config not written:\n%s", data)
	}
	if r := h.gw.Registration(); r.Active || r.Ended != "registered" {
		t.Errorf("registration should end once the owner is registered: %+v", r)
	}
	gotOwnerEvent := false
	for len(ev) > 0 {
		if e := <-ev; e.Type == "owner" {
			gotOwnerEvent = true
		}
	}
	if !gotOwnerEvent {
		t.Error("the app should be told about the change")
	}

	if err := h.gw.ClearOwner(); err != nil {
		t.Fatal(err)
	}
	if st := h.gw.Status(0); st.Owner != "" || st.NotifyDM {
		t.Errorf("owner not cleared: %+v", st)
	}
}

func TestRegistrationEnds(t *testing.T) {
	h := newOwnerHarness(t, "")
	reg, _ := h.gw.StartRegistration()
	for i := range maxWrongCodes {
		code := fmt.Sprintf("%06d", i)
		if code == reg.Code {
			code = "999999"
		}
		h.gw.HandleMessage(dm(fmt.Sprintf("400.%d", i+1), code))
	}
	if r := h.gw.Registration(); r.Active || r.Ended != "wrong_codes" {
		t.Errorf("five wrong codes should end the registration: %+v", r)
	}
	h.gw.HandleMessage(dm("400.9", reg.Code))
	if h.gw.Status(0).Owner != "" {
		t.Error("a code after the registration ended should not register")
	}

	reg, _ = h.gw.StartRegistration()
	h.advance(registrationTTL + time.Second)
	h.gw.expireRegistration()
	if r := h.gw.Registration(); r.Active || r.Ended != "expired" {
		t.Errorf("the registration should end after 10 minutes: %+v", r)
	}
}

func TestOwnerNotices(t *testing.T) {
	h := newOwnerHarness(t, "  owner: UALEX\n  notify_dm: true\n  notify_dm_failures: true")
	// The owner's DM channel is learned from their DMs.
	h.gw.HandleMessage(dm("500.1", "hello"))

	h.gw.NotifyRuleChanged("C1", config.KindStartsWith, "review")
	h.gw.NotifyRuleChanged(config.ChannelDM, config.KindStartsWith, "todo")

	h.mu.Lock()
	h.fail = &handler.ExitError{Reason: handler.ReasonSignedOut}
	h.mu.Unlock()
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "500.2", User: "UALEX", Text: "<@UBOT> review x"})
	waitFor(t, "failure notice", func() bool {
		for _, p := range h.api.postsIn("") {
			if strings.Contains(p, "failed.") {
				return true
			}
		}
		return false
	})
	// The sign-out is told once.
	waitFor(t, "sign-out notice", func() bool {
		return count(h.api.postsIn(""), config.DefaultMessages.ClaudeSignedOut) == 1
	})
	h.gw.setClaudeSignedIn(true)
	h.gw.setClaudeSignedIn(false)
	waitFor(t, "sign-out notice after signing in again", func() bool {
		return count(h.api.postsIn(""), config.DefaultMessages.ClaudeSignedOut) == 2
	})

	h.api.mu.Lock()
	defer h.api.mu.Unlock()
	var texts []string
	for _, p := range h.api.posts {
		if p.thread == "" {
			if p.channel != "D0ALEX" {
				t.Errorf("notice sent to %s, want the owner's DM", p.channel)
			}
			texts = append(texts, p.text)
		}
	}
	want := []string{
		"<#C1> “review” was changed through MCP",
		"DM “todo” was changed through MCP",
		"⚠️ <#C1> “review” failed. Claude Code isn't signed in. <https://example.slack.com/archives/C1/p5002|Thread>",
	}
	for _, w := range want {
		if count(texts, w) != 1 {
			t.Errorf("missing notice %q in %q", w, texts)
		}
	}
}

func TestNoNoticesWhenOff(t *testing.T) {
	h := newOwnerHarness(t, "  owner: UALEX")
	h.gw.NotifyRuleChanged("C1", config.KindStartsWith, "review")
	h.mu.Lock()
	h.fail = errors.New("boom")
	h.mu.Unlock()
	h.gw.HandleMessage(event.Message{Channel: "C1", TS: "600.1", User: "UALEX", Text: "<@UBOT> review x"})
	waitFor(t, "error reply in the thread", func() bool { return len(h.api.postsIn("600.1")) == 1 })
	if p := h.api.postsIn(""); len(p) != 0 {
		t.Errorf("notify_dm is off: no notices expected, got %v", p)
	}
}

func TestTOTP(t *testing.T) {
	// RFC 6238 test vector (SHA1, 59 s): 94287082 → last six digits.
	secret := []byte("12345678901234567890")
	if got := totp(secret, time.Unix(59, 0)); got != "287082" {
		t.Errorf("totp = %s", got)
	}
	for in, want := range map[string]string{"287082": "287082", "287 082": "287082", "２８７０８２": "287082", "28708": "", "todo 1": ""} {
		got, ok := normalizeCode(in)
		if (want == "") == ok || got != want {
			t.Errorf("normalizeCode(%q) = %q, %v", in, got, ok)
		}
	}
}

func count(list []string, s string) int {
	n := 0
	for _, x := range list {
		if x == s {
			n++
		}
	}
	return n
}
