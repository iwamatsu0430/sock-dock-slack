// Package gateway ties everything together: it filters and routes incoming
// messages, binds threads to routes, runs handlers per thread and posts replies.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/dispatch"
	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/handler"
	"github.com/iwamatsu0430/sock-dock-slack/internal/router"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
	"github.com/iwamatsu0430/sock-dock-slack/internal/textutil"
)

// Options configure a Gateway.
type Options struct {
	ConfigPath       string
	Store            *store.Store
	API              slackio.API
	Log              *slog.Logger
	BotToken         string
	SystemPromptFile string
	Version          string

	// Overridable for tests.
	RunCommand func(context.Context, *config.Route, handler.Input, handler.Env) (handler.Result, error)
	RunClaude  func(context.Context, *config.Route, handler.Input, handler.ClaudeOptions, handler.Env) (handler.Result, error)
	Now        func() time.Time
}

// Gateway is the running core.
type Gateway struct {
	opts Options
	log  *slog.Logger
	cfg  atomic.Pointer[config.Config]
	disp *dispatch.Dispatcher[event.Message]
	self router.Self

	// runCtx is cancelled on shutdown; handlers run under it.
	runCtx    context.Context
	cancelRun context.CancelFunc

	mu      sync.Mutex
	conn    connInfo
	running map[string]RunningTurn
	paused  map[string]bool
	subs    map[chan Event]struct{}
	started time.Time
	ident   slackio.Identity

	// lastPrune is only touched by the Watch goroutine.
	lastPrune time.Time
}

type connInfo struct {
	connected      bool
	lastConnected  time.Time
	lastDisconnect time.Time
	lastEvent      time.Time
	lastError      string
}

// RunningTurn is a handler run in progress.
type RunningTurn struct {
	TurnID    int64     `json:"turn_id"`
	Route     string    `json:"route"`
	Channel   string    `json:"channel"`
	ThreadTS  string    `json:"thread_ts"`
	Permalink string    `json:"permalink,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// Event is pushed to subscribers (the control API's event stream).
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

const stateKeyPaused = "paused_routes"

// New creates a gateway with an already-loaded config.
func New(opts Options, cfg *config.Config) *Gateway {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.RunCommand == nil {
		opts.RunCommand = handler.RunCommand
	}
	if opts.RunClaude == nil {
		opts.RunClaude = handler.RunClaude
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		opts:      opts,
		log:       opts.Log,
		runCtx:    ctx,
		cancelRun: cancel,
		running:   map[string]RunningTurn{},
		paused:    map[string]bool{},
		subs:      map[chan Event]struct{}{},
		started:   opts.Now(),
	}
	g.cfg.Store(cfg)
	g.disp = dispatch.New(cfg.Settings.MaxConcurrency, g.process)
	return g
}

// Config returns the active config.
func (g *Gateway) Config() *config.Config { return g.cfg.Load() }

// Start identifies the bot, restores paused routes and cleans up turns left
// running by a previous process.
func (g *Gateway) Start(ctx context.Context) error {
	id, err := g.opts.API.AuthTest(ctx)
	if err != nil {
		return fmt.Errorf("slack auth.test failed (check SLACK_BOT_TOKEN): %w", err)
	}
	g.mu.Lock()
	g.ident = id
	g.self = router.Self{UserID: id.UserID, BotID: id.BotID}
	g.mu.Unlock()

	var paused []string
	if _, err := g.opts.Store.GetState(ctx, stateKeyPaused, &paused); err != nil {
		g.log.Warn("could not read paused routes", "err", err)
	}
	g.mu.Lock()
	for _, n := range paused {
		g.paused[n] = true
	}
	g.mu.Unlock()

	g.recoverInterrupted(ctx)
	return nil
}

func (g *Gateway) recoverInterrupted(ctx context.Context) {
	turns, err := g.opts.Store.RunningTurns(ctx)
	if err != nil {
		g.log.Warn("could not list running turns", "err", err)
		return
	}
	cfg := g.cfg.Load()
	for _, t := range turns {
		g.log.Info("turn was interrupted by a restart", "turn", t.ID, "route", t.Route)
		_ = g.opts.API.PostMessage(ctx, t.Channel, t.ThreadTS, config.Render(cfg.Settings.Messages.Interrupted, t.Route, 0))
		for _, ts := range t.MessageTS {
			_ = g.opts.API.RemoveReaction(ctx, t.Channel, ts, cfg.Settings.AckReaction)
		}
		_ = g.opts.Store.FinishTurn(ctx, t.ID, store.StatusInterrupted, "gateway restarted", 0)
	}
}

// HandleMessage is called for every message event from Slack.
func (g *Gateway) HandleMessage(m event.Message) {
	ctx := g.runCtx
	g.mu.Lock()
	self := g.self
	g.mu.Unlock()
	if !router.Accept(m, self) {
		return
	}
	fresh, err := g.opts.Store.MarkSeen(ctx, m.Channel, m.TS)
	if err != nil {
		g.log.Error("dedup check failed", "err", err)
		return
	}
	if !fresh {
		return
	}
	g.mu.Lock()
	g.conn.lastEvent = g.opts.Now()
	g.mu.Unlock()

	cfg := g.cfg.Load()
	var r *config.Route
	if m.IsTopLevel() {
		route, groups := cfg.Match(m.Channel, m.Text)
		if route == nil || g.isPaused(route.Name) {
			return
		}
		if err := g.opts.Store.BindThread(ctx, store.Thread{
			Channel: m.Channel, ThreadTS: m.TS, Route: route.Name, Groups: groups,
		}); err != nil {
			g.log.Error("could not bind thread", "err", err)
			return
		}
		g.log.Info("thread started", "route", route.Name, "channel", m.Channel, "ts", m.TS)
		r = route
	} else {
		th, err := g.opts.Store.GetThread(ctx, m.Channel, m.ThreadTS)
		if err != nil {
			g.log.Error("thread lookup failed", "err", err)
			return
		}
		if th == nil {
			return
		}
		ttl := time.Duration(cfg.Settings.ThreadTTLDays) * 24 * time.Hour
		if g.opts.Now().Sub(th.LastActiveAt) > ttl {
			return
		}
		r = cfg.Route(th.Route)
	}
	coalesce := r != nil && r.Handler.Type == config.TypeClaude
	g.disp.Submit(router.ThreadKey(m.Channel, m.ThreadRoot()), m, coalesce)
	g.broadcast(Event{Type: "queued", Data: map[string]string{"channel": m.Channel, "thread_ts": m.ThreadRoot()}})
}

// process runs one batch of messages for a thread.
func (g *Gateway) process(key string, batch []event.Message) {
	ctx := context.WithoutCancel(g.runCtx)
	first := batch[0]
	channel, threadTS := first.Channel, first.ThreadRoot()
	cfg := g.cfg.Load()
	msgs := cfg.Settings.Messages
	api := g.opts.API

	th, err := g.opts.Store.GetThread(ctx, channel, threadTS)
	if err != nil || th == nil {
		g.log.Error("thread vanished", "key", key, "err", err)
		return
	}
	r := cfg.Route(th.Route)
	if r == nil {
		_ = api.PostMessage(ctx, channel, threadTS, config.Render(msgs.RouteMissing, th.Route, 0))
		return
	}
	if g.isPaused(r.Name) {
		_ = api.PostMessage(ctx, channel, threadTS, config.Render(msgs.RoutePaused, r.Name, r.Handler.TimeoutSec))
		return
	}

	var tsList []string
	var inMsgs []handler.InputMessage
	for _, m := range batch {
		tsList = append(tsList, m.TS)
		inMsgs = append(inMsgs, handler.InputMessage{TS: m.TS, User: m.User, Text: m.Text, Files: m.Files})
		if err := api.AddReaction(ctx, channel, m.TS, cfg.Settings.AckReaction); err != nil {
			g.log.Warn("could not add reaction", "err", err)
		}
	}
	defer func() {
		for _, ts := range tsList {
			if err := api.RemoveReaction(ctx, channel, ts, cfg.Settings.AckReaction); err != nil {
				g.log.Warn("could not remove reaction", "err", err)
			}
		}
	}()

	permalink := th.Permalink
	if permalink == "" {
		if p, err := api.Permalink(ctx, channel, threadTS); err == nil {
			permalink = p
		} else {
			g.log.Warn("could not get permalink", "err", err)
		}
	}

	turnID, err := g.opts.Store.StartTurn(ctx, store.Turn{Channel: channel, ThreadTS: threadTS, MessageTS: tsList, Route: r.Name})
	if err != nil {
		g.log.Error("could not record turn", "err", err)
		return
	}
	rt := RunningTurn{TurnID: turnID, Route: r.Name, Channel: channel, ThreadTS: threadTS, Permalink: permalink, StartedAt: g.opts.Now()}
	g.setRunning(key, &rt)
	g.broadcast(Event{Type: "turn_started", Data: rt})

	in := handler.Input{
		Route: r.Name, Turn: th.Turns + 1, Channel: channel, ThreadTS: threadTS,
		Permalink: permalink, Groups: th.Groups, Messages: inMsgs,
	}
	env := handler.Env{BotToken: g.opts.BotToken, Path: cfg.SearchPath()}
	log := g.log.With("route", r.Name, "turn", turnID, "channel", channel, "thread_ts", threadTS)
	log.Info("handler started", "type", r.Handler.Type, "messages", len(batch))
	start := g.opts.Now()

	var res handler.Result
	switch r.Handler.Type {
	case config.TypeClaude:
		res, err = g.opts.RunClaude(g.runCtx, r, in, handler.ClaudeOptions{
			Command:          cfg.ClaudeCommandFor(r),
			SystemPromptFile: g.opts.SystemPromptFile,
			SessionID:        th.SessionID,
			InstructionsFile: g.instructionsFile(r, log),
		}, env)
	default:
		res, err = g.opts.RunCommand(g.runCtx, r, in, env)
	}

	status, errMsg := store.StatusOK, ""
	switch {
	case err == nil:
		if res.ResumeFailed {
			_ = api.PostMessage(ctx, channel, threadTS, msgs.ResumeFailed)
		}
		text := res.Text
		if r.Handler.Type == config.TypeClaude {
			text = textutil.ToMrkdwn(text)
		}
		for _, chunk := range textutil.Split(text, textutil.DefaultLimit) {
			if perr := api.PostMessage(ctx, channel, threadTS, chunk); perr != nil {
				status, errMsg = store.StatusError, "post failed: "+perr.Error()
				log.Error("could not post reply", "err", perr)
				break
			}
		}
	case g.runCtx.Err() != nil:
		status, errMsg = store.StatusInterrupted, "gateway shutting down"
		_ = api.PostMessage(ctx, channel, threadTS, config.Render(msgs.Interrupted, r.Name, r.Handler.TimeoutSec))
	case errors.Is(err, handler.ErrTimeout):
		status, errMsg = store.StatusTimeout, err.Error()
		_ = api.PostMessage(ctx, channel, threadTS, config.Render(msgs.Timeout, r.Name, r.Handler.TimeoutSec))
	default:
		status, errMsg = store.StatusError, err.Error()
		var ee *handler.ExitError
		if errors.As(err, &ee) && ee.Stderr != "" {
			log.Error("handler failed", "err", err, "stderr", ee.Stderr)
		} else {
			log.Error("handler failed", "err", err)
		}
		_ = api.PostMessage(ctx, channel, threadTS, config.Render(msgs.Error, r.Name, r.Handler.TimeoutSec))
	}
	log.Info("handler finished", "status", status, "duration", g.opts.Now().Sub(start).Round(time.Millisecond))

	if uerr := g.opts.Store.UpdateThreadAfterTurn(ctx, channel, threadTS, res.SessionID, permalink); uerr != nil {
		log.Error("could not update thread", "err", uerr)
	}
	if ferr := g.opts.Store.FinishTurn(ctx, turnID, status, errMsg, res.CostUSD); ferr != nil {
		log.Error("could not finish turn", "err", ferr)
	}
	g.setRunning(key, nil)
	g.broadcast(Event{Type: "turn_finished", Data: map[string]any{"turn_id": turnID, "route": r.Name, "status": status}})
}

// instructionsFile writes a route's inline instructions next to the gateway
// prompt and returns the path ("" when the route has none).
func (g *Gateway) instructionsFile(r *config.Route, log *slog.Logger) string {
	if strings.TrimSpace(r.Handler.Instructions) == "" || g.opts.SystemPromptFile == "" {
		return ""
	}
	p := filepath.Join(filepath.Dir(g.opts.SystemPromptFile), "route-"+r.Name+".md")
	if err := os.WriteFile(p, []byte(r.Handler.Instructions), 0o600); err != nil {
		log.Warn("could not write route instructions", "err", err)
		return ""
	}
	return p
}

// Reload re-reads the config file. On error the current config stays active.
func (g *Gateway) Reload() error {
	cfg, err := config.Load(g.opts.ConfigPath)
	if err != nil {
		g.log.Error("config reload failed; keeping the current config", "err", err)
		return err
	}
	old := g.cfg.Swap(cfg)
	if old.Settings.MaxConcurrency != cfg.Settings.MaxConcurrency {
		g.log.Warn("max_concurrency changes take effect after a restart")
	}
	g.log.Info("config reloaded", "routes", len(cfg.Routes))
	g.broadcast(Event{Type: "config_reloaded"})
	return nil
}

// SetPaused pauses or resumes a route.
func (g *Gateway) SetPaused(ctx context.Context, name string, paused bool) error {
	if g.cfg.Load().Route(name) == nil {
		return fmt.Errorf("no route named %q", name)
	}
	g.mu.Lock()
	if paused {
		g.paused[name] = true
	} else {
		delete(g.paused, name)
	}
	list := make([]string, 0, len(g.paused))
	for n := range g.paused {
		list = append(list, n)
	}
	g.mu.Unlock()
	sort.Strings(list)
	if err := g.opts.Store.SetState(ctx, stateKeyPaused, list); err != nil {
		return err
	}
	g.broadcast(Event{Type: "route_paused", Data: map[string]any{"route": name, "paused": paused}})
	return nil
}

func (g *Gateway) isPaused(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused[name]
}

// OnConnState records Socket Mode connection changes.
func (g *Gateway) OnConnState(s slackio.ConnState) {
	g.mu.Lock()
	changed := g.conn.connected != s.Connected
	now := g.opts.Now()
	if s.Connected {
		g.conn.connected = true
		g.conn.lastConnected = now
		g.conn.lastError = ""
	} else {
		if g.conn.connected {
			g.conn.lastDisconnect = now
		}
		g.conn.connected = false
		if s.Err != nil {
			g.conn.lastError = s.Err.Error()
		}
	}
	g.mu.Unlock()
	if changed {
		g.broadcast(Event{Type: "connection", Data: map[string]bool{"connected": s.Connected}})
	}
}

// Shutdown stops accepting messages, cancels running handlers (they are told
// to stop and reported as interrupted) and waits for them to finish.
func (g *Gateway) Shutdown(ctx context.Context) error {
	g.cancelRun()
	return g.disp.Shutdown(ctx)
}

func (g *Gateway) setRunning(key string, rt *RunningTurn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if rt == nil {
		delete(g.running, key)
	} else {
		g.running[key] = *rt
	}
}

// Subscribe returns a channel of events and a function to unsubscribe.
func (g *Gateway) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 32)
	g.mu.Lock()
	g.subs[ch] = struct{}{}
	g.mu.Unlock()
	return ch, func() {
		g.mu.Lock()
		if _, ok := g.subs[ch]; ok {
			delete(g.subs, ch)
			close(ch)
		}
		g.mu.Unlock()
	}
}

func (g *Gateway) broadcast(e Event) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for ch := range g.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop
		}
	}
}
