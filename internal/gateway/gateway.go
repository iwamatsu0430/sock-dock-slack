// Package gateway ties everything together: it filters and routes incoming
// messages, binds threads to routes, runs handlers per thread and posts replies.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
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
	"github.com/iwamatsu0430/sock-dock-slack/internal/updates"
)

// Options configure a Gateway.
type Options struct {
	ConfigPath       string
	Store            *store.Store
	API              slackio.API
	Log              *slog.Logger
	SystemPromptFile string
	Version          string
	// FilesDir is where turns keep attached files while they run
	// (docs/design.md §3.6).
	FilesDir string

	// Overridable for tests.
	RunCommand func(context.Context, *config.Route, handler.Input, handler.Env) (handler.Result, error)
	RunClaude  func(context.Context, *config.Route, handler.Input, handler.ClaudeOptions, handler.Env) (handler.Result, error)
	// ClaudeSignedIn asks Claude Code whether it is signed in.
	ClaudeSignedIn func(ctx context.Context, command []string, pathEnv string) (signedIn, ok bool)
	Now            func() time.Time
	// UpdateURL and UpdateClient are where a newer version is looked up
	// (default: GitHub's latest release).
	UpdateURL    string
	UpdateClient *http.Client
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
	// stops cancels a running turn by id; stopped marks turns stopped by the user.
	stops   map[int64]context.CancelFunc
	stopped map[int64]bool
	paused  map[string]bool
	// heard holds channels whose posts reached SockDock since it started.
	heard   map[string]bool
	claude  claudeAuth
	subs    map[chan Event]struct{}
	started time.Time
	ident   slackio.Identity
	// scopes are the Bot Token's scopes (nil when unknown).
	scopes []string
	// reg is the owner registration in progress; ownerDM is the owner's DM
	// channel (owner.go).
	// update is a newer release (update.go); updateNext is when to check
	// next.
	update         *updates.Release
	updateNext     time.Time
	updateChecking bool
	reg            *registration
	regEnded       string
	ownerDM        ownerDM
	// cfgWrite serializes the gateway's own edits of the config file;
	// claudeNotice serializes the Claude Code sign-out notice.
	cfgWrite     sync.Mutex
	claudeNotice sync.Mutex

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
	if opts.ClaudeSignedIn == nil {
		opts.ClaudeSignedIn = handler.ClaudeSignedIn
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	if opts.UpdateURL == "" {
		opts.UpdateURL = updates.LatestURL
	}
	ctx, cancel := context.WithCancel(context.Background())
	g := &Gateway{
		opts:      opts,
		log:       opts.Log,
		runCtx:    ctx,
		cancelRun: cancel,
		running:   map[string]RunningTurn{},
		stops:     map[int64]context.CancelFunc{},
		stopped:   map[int64]bool{},
		paused:    map[string]bool{},
		heard:     map[string]bool{},
		subs:      map[chan Event]struct{}{},
		started:   opts.Now(),
	}
	g.cfg.Store(cfg)
	g.disp = dispatch.New(cfg.Settings.MaxConcurrency, g.process)
	return g
}

// Config returns the active config.
func (g *Gateway) Config() *config.Config { return g.cfg.Load() }

// API returns the Slack Web API client.
func (g *Gateway) API() slackio.API { return g.opts.API }

// Announce sends an event to subscribers (e.g. a rule changed through the
// control API).
func (g *Gateway) Announce(e Event) { g.broadcast(e) }

// Start identifies the bot, restores paused routes and cleans up turns left
// running by a previous process.
func (g *Gateway) Start(ctx context.Context) error {
	id, err := g.opts.API.AuthTest(ctx)
	if err != nil {
		return fmt.Errorf("slack auth.test failed (check SLACK_BOT_TOKEN): %w", err)
	}
	var scopes []string
	if l, ok := g.opts.API.(slackio.ScopeLister); ok {
		if scopes, err = l.Scopes(ctx); err != nil {
			g.log.Warn("could not read the bot token's scopes", "err", err)
		}
	}
	g.mu.Lock()
	g.ident = id
	g.self = router.Self{UserID: id.UserID, BotID: id.BotID}
	g.scopes = scopes
	g.mu.Unlock()

	g.loadUpdate(ctx)
	g.mu.Lock()
	g.updateNext = g.opts.Now().Add(updateFirstCheck)
	g.mu.Unlock()

	var dm ownerDM
	if _, err := g.opts.Store.GetState(ctx, stateKeyOwnerDM, &dm); err != nil {
		g.log.Warn("could not read the owner's DM channel", "err", err)
	}
	g.mu.Lock()
	g.ownerDM = dm
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
	g.clearFiles()
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
	// Remember that posts from this channel reach SockDock at all, so the app
	// can tell "nothing arrived" apart from "nothing matched". DMs count as
	// one channel, "dm".
	if m.IsIM() {
		g.heard[config.ChannelDM] = true
	} else if m.Channel != "" {
		g.heard[m.Channel] = true
	}
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
	if m.IsIM() {
		if m.User != "" && m.User == cfg.Settings.Owner {
			g.rememberOwnerDM(ctx, m.User, m.Channel)
		}
		// While the owner is being registered, a DM of six digits is a code.
		if m.IsTopLevel() && g.handleCode(ctx, m) {
			return
		}
	}
	var r *config.Route
	if m.IsTopLevel() {
		// Rules are tried top to bottom; a rule that is off, or that the
		// poster may not run, is passed over.
		route, groups := cfg.MatchWith(m.Channel, m.Text, config.MatchOptions{
			BotUserID: self.UserID, Skip: g.isPaused, ByUser: true, User: m.User, IM: m.IsIM(),
		})
		if route == nil {
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
		// Replies from people who may not run the rule are dropped: they
		// neither start a run nor reach a later one (docs/design.md §3.4).
		if r != nil && !r.Allows(m.User) {
			g.log.Debug("reply from someone the rule does not allow", "route", r.Name, "channel", m.Channel, "ts", m.TS)
			return
		}
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
		inMsgs = append(inMsgs, handler.InputMessage{TS: m.TS, User: m.User, Text: m.Text})
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

	env := handler.Env{Path: cfg.SearchPath()}
	turnCtx, cancelTurn := context.WithCancel(g.runCtx)
	defer cancelTurn()
	g.mu.Lock()
	g.stops[turnID] = cancelTurn
	g.mu.Unlock()
	log := g.log.With("route", r.Name, "turn", turnID, "channel", channel, "thread_ts", threadTS)
	start := g.opts.Now()

	// Attached files are fetched within the turn's time limit and removed
	// when the turn ends (docs/design.md §3.6).
	fetchCtx, cancelFetch := context.WithCancel(turnCtx)
	if r.Handler.TimeoutSec > 0 {
		cancelFetch()
		fetchCtx, cancelFetch = context.WithTimeout(turnCtx, time.Duration(r.Handler.TimeoutSec)*time.Second)
	}
	files := g.fetchFiles(fetchCtx, turnID, batch, cfg.Settings.MaxFileMB, log)
	timedOut := fetchCtx.Err() == context.DeadlineExceeded
	cancelFetch()
	if files.dir != "" {
		defer func() {
			if err := os.RemoveAll(files.dir); err != nil {
				log.Warn("could not remove attached files", "err", err)
			}
		}()
	}
	if files.problem != "" {
		if err := g.opts.Store.SetFileProblem(ctx, turnID, files.problem); err != nil {
			log.Warn("could not record the file problem", "err", err)
		}
	}
	for i := range inMsgs {
		inMsgs[i].Files = files.byTS[inMsgs[i].TS]
	}
	in := handler.Input{
		Route: r.Name, Turn: th.Turns + 1, Channel: channel, ThreadTS: threadTS,
		Permalink: permalink, Groups: th.Groups, Messages: inMsgs,
	}
	// The handler gets what is left of the time limit.
	run := r
	if spent := g.opts.Now().Sub(start); spent >= time.Second && r.Handler.TimeoutSec > 0 {
		rc := *r
		rc.Handler.TimeoutSec = max(1, r.Handler.TimeoutSec-int(spent/time.Second))
		run = &rc
	}
	log.Info("handler started", "type", r.Handler.Type, "messages", len(batch), "files_dir", files.dir)

	var res handler.Result
	switch {
	case timedOut:
		err = handler.ErrTimeout
	case r.Handler.Type == config.TypeClaude:
		res, err = g.opts.RunClaude(turnCtx, run, in, handler.ClaudeOptions{
			Command:          cfg.ClaudeCommandFor(r),
			SystemPromptFile: g.opts.SystemPromptFile,
			SessionID:        th.SessionID,
			InstructionsFile: g.instructionsFile(r, log),
			FilesDir:         files.dir,
		}, env)
	default:
		res, err = g.opts.RunCommand(turnCtx, run, in, env)
	}
	g.mu.Lock()
	stoppedByUser := g.stopped[turnID]
	delete(g.stops, turnID)
	delete(g.stopped, turnID)
	g.mu.Unlock()
	if r.Handler.Type == config.TypeClaude {
		var ee *handler.ExitError
		switch {
		case err == nil:
			g.setClaudeSignedIn(true)
		case errors.As(err, &ee) && ee.Reason == handler.ReasonSignedOut:
			g.setClaudeSignedIn(false)
		}
	}

	status, errMsg := store.StatusOK, ""
	switch {
	case stoppedByUser && g.runCtx.Err() == nil:
		status, errMsg = store.StatusStopped, ""
		_ = api.PostMessage(ctx, channel, threadTS, config.Render(msgs.Stopped, r.Name, r.Handler.TimeoutSec))
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
	if status == store.StatusError || status == store.StatusTimeout {
		g.notifyRunFailed(ctx, cfg, r, status, err, permalink)
	}

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

// StopTurn stops a running turn. The thread gets the "stopped" message.
func (g *Gateway) StopTurn(id int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	cancel, ok := g.stops[id]
	if !ok {
		return fmt.Errorf("turn %d is not running", id)
	}
	g.stopped[id] = true
	cancel()
	return nil
}

// RouteTurns lists the recent turns of one route.
func (g *Gateway) RouteTurns(ctx context.Context, route string, limit int) ([]store.Turn, error) {
	return g.opts.Store.ListRouteTurns(ctx, route, limit)
}

// Reload re-reads the config file. On error the current config stays active.
// Routes with problems are left out (see Status().Broken).
func (g *Gateway) Reload() error {
	cfg, err := config.LoadLenient(g.opts.ConfigPath)
	if err != nil {
		g.log.Error("config reload failed; keeping the current config", "err", err)
		return err
	}
	for _, b := range cfg.Broken {
		g.log.Warn("route left out", "route", b.Name, "problems", strings.Join(b.Problems, "; "))
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
