package gateway

import (
	"context"
	"sort"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// Status is the snapshot returned by the control API.
type Status struct {
	Version          string           `json:"version"`
	PID              int              `json:"pid"`
	StartedAt        time.Time        `json:"started_at"`
	Connected        bool             `json:"connected"`
	LastConnectedAt  *time.Time       `json:"last_connected_at,omitempty"`
	LastDisconnectAt *time.Time       `json:"last_disconnect_at,omitempty"`
	LastEventAt      *time.Time       `json:"last_event_at,omitempty"`
	LastError        string           `json:"last_error,omitempty"`
	Identity         slackio.Identity `json:"identity"`
	ConfigPath       string           `json:"config_path"`
	Routes           []RouteStatus    `json:"routes"`
	// Broken lists routes left out because of problems.
	Broken  []config.BrokenRoute `json:"broken,omitempty"`
	Running []RunningTurn        `json:"running"`
	// Heard lists the channels whose posts have reached SockDock since it
	// started.
	Heard []string `json:"heard"`
	// ClaudeSignedIn is whether Claude Code is signed in, when known.
	ClaudeSignedIn *bool `json:"claude_signed_in,omitempty"`
	Pending        int   `json:"pending"`
	// Scopes are the Bot Token's scopes (absent when unknown).
	Scopes []string `json:"scopes,omitempty"`
	// Owner is the registered owner's member ID; NotifyDM and
	// NotifyDMFailures are the DM notice settings.
	Owner            string `json:"owner,omitempty"`
	NotifyDM         bool   `json:"notify_dm"`
	NotifyDMFailures bool   `json:"notify_dm_failures"`
}

// HasScope reports whether the Bot Token has the scope. It is true when the
// scopes are not known, so nothing is reported missing by mistake.
func (s Status) HasScope(scope string) bool {
	if s.Scopes == nil {
		return true
	}
	for _, x := range s.Scopes {
		if x == scope {
			return true
		}
	}
	return false
}

// RouteStatus summarizes a route.
type RouteStatus struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	// Condition is the route's condition as written in the config.
	Condition string `json:"condition"`
	Paused    bool   `json:"paused"`
}

// Status returns the current state.
func (g *Gateway) Status(pid int) Status {
	cfg := g.cfg.Load()
	g.mu.Lock()
	defer g.mu.Unlock()
	s := Status{
		Version:    g.opts.Version,
		PID:        pid,
		StartedAt:  g.started,
		Connected:  g.conn.connected,
		LastError:  g.conn.lastError,
		Identity:   g.ident,
		ConfigPath: cfg.Path,
		Pending:    g.disp.Pending(),
	}
	s.LastConnectedAt = timePtr(g.conn.lastConnected)
	s.LastDisconnectAt = timePtr(g.conn.lastDisconnect)
	s.LastEventAt = timePtr(g.conn.lastEvent)
	for _, r := range cfg.Routes {
		s.Routes = append(s.Routes, RouteStatus{
			Name: r.Name, Type: r.Handler.Type, Channel: r.Channel, Condition: r.Condition(), Paused: g.paused[r.Name],
		})
	}
	s.Broken = cfg.Broken
	for _, rt := range g.running {
		s.Running = append(s.Running, rt)
	}
	sort.Slice(s.Running, func(i, j int) bool { return s.Running[i].StartedAt.Before(s.Running[j].StartedAt) })
	s.Heard = []string{}
	for ch := range g.heard {
		s.Heard = append(s.Heard, ch)
	}
	sort.Strings(s.Heard)
	s.ClaudeSignedIn = g.claudeSignedInLocked()
	s.Scopes = g.scopes
	s.Owner, s.NotifyDM, s.NotifyDMFailures = cfg.Settings.Owner, cfg.Settings.NotifyDM, cfg.Settings.NotifyDMFailures
	return s
}

// Threads lists recent threads.
func (g *Gateway) Threads(ctx context.Context, limit int) ([]store.Thread, error) {
	return g.opts.Store.ListThreads(ctx, limit)
}

// Turns lists recent turns.
func (g *Gateway) Turns(ctx context.Context, limit int) ([]store.Turn, error) {
	return g.opts.Store.ListTurns(ctx, limit)
}

// connectedSince reports whether the connection is up, and when it last
// changed state.
func (g *Gateway) connectedSince() (bool, time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.conn.connected {
		return true, g.conn.lastConnected
	}
	last := g.conn.lastDisconnect
	if last.IsZero() {
		last = g.started
	}
	return false, last
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
