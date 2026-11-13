package gateway

import (
	"context"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// claudeAuth is what SockDock knows about Claude Code's sign-in (docs/ia.md
// §11-21). Runs update it too: a success means signed in, an auth failure
// means signed out.
type claudeAuth struct {
	known    bool
	signedIn bool
	checked  time.Time
	checking bool
}

// hasClaudeRoutes reports whether any rule runs Claude Code.
func hasClaudeRoutes(cfg *config.Config) bool {
	for _, r := range cfg.Routes {
		if r.Handler.Type == config.TypeClaude {
			return true
		}
	}
	return false
}

// CheckClaude asks Claude Code whether it is signed in, unless it was checked
// within maxAge, and returns the answer (nil when unknown or no rule runs
// Claude Code). Only one check runs at a time; others get the last answer.
func (g *Gateway) CheckClaude(ctx context.Context, maxAge time.Duration) *bool {
	cfg := g.cfg.Load()
	if !hasClaudeRoutes(cfg) {
		return nil
	}
	g.mu.Lock()
	fresh := g.claude.known && g.opts.Now().Sub(g.claude.checked) < maxAge
	if fresh || g.claude.checking {
		v := g.claudeSignedInLocked()
		g.mu.Unlock()
		return v
	}
	g.claude.checking = true
	g.mu.Unlock()

	signedIn, ok := g.opts.ClaudeSignedIn(ctx, cfg.Settings.ClaudeCommand, cfg.SearchPath())

	g.mu.Lock()
	g.claude.checking = false
	g.claude.checked = g.opts.Now()
	g.mu.Unlock()
	if ok {
		g.setClaudeSignedIn(signedIn)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.claudeSignedInLocked()
}

// setClaudeSignedIn records the sign-in state and tells subscribers when it
// changed.
func (g *Gateway) setClaudeSignedIn(signedIn bool) {
	g.mu.Lock()
	changed := !g.claude.known || g.claude.signedIn != signedIn
	g.claude.known, g.claude.signedIn = true, signedIn
	g.mu.Unlock()
	if changed {
		g.broadcast(Event{Type: "claude_auth", Data: map[string]bool{"signed_in": signedIn}})
	}
}

func (g *Gateway) claudeSignedInLocked() *bool {
	if !g.claude.known {
		return nil
	}
	v := g.claude.signedIn
	return &v
}
