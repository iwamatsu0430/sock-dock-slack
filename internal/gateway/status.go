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
	Pending int                  `json:"pending"`
}

// RouteStatus summarizes a route.
type RouteStatus struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Match   string `json:"match"`
	Paused  bool   `json:"paused"`
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
			Name: r.Name, Type: r.Handler.Type, Channel: r.Channel, Match: r.Match, Paused: g.paused[r.Name],
		})
	}
	s.Broken = cfg.Broken
	for _, rt := range g.running {
		s.Running = append(s.Running, rt)
	}
	sort.Slice(s.Running, func(i, j int) bool { return s.Running[i].StartedAt.Before(s.Running[j].StartedAt) })
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
