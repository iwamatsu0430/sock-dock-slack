package gateway

import (
	"context"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// claudeAuth is what SockDock knows about an agent's sign-in.
// Runs update it too: a success means signed
// in, an auth failure means signed out.
type claudeAuth struct {
	known    bool
	signedIn bool
	checked  time.Time
	checking bool
}

// hasClaudeRoutes reports whether any rule runs Claude Code.
func hasClaudeRoutes(cfg *config.Config) bool { return hasAgentRoutes(cfg, config.TypeClaude) }

// hasAgentRoutes reports whether any rule runs the agent.
func hasAgentRoutes(cfg *config.Config, agent string) bool {
	for _, r := range cfg.Routes {
		if r.Handler.Type == agent {
			return true
		}
	}
	return false
}

// CheckClaude asks Claude Code whether it is signed in, unless it was checked
// within maxAge, and returns the answer (nil when unknown or no rule runs
// Claude Code). Only one check runs at a time; others get the last answer.
func (g *Gateway) CheckClaude(ctx context.Context, maxAge time.Duration) *bool {
	return g.checkAgent(ctx, config.TypeClaude, maxAge)
}

// CheckAgents checks every agent that a rule runs (see CheckClaude) and
// returns the known answers by agent type.
func (g *Gateway) CheckAgents(ctx context.Context, maxAge time.Duration) map[string]bool {
	out := map[string]bool{}
	for _, a := range config.Agents {
		if v := g.checkAgent(ctx, a, maxAge); v != nil {
			out[a] = *v
		}
	}
	return out
}

func (g *Gateway) checkAgent(ctx context.Context, agent string, maxAge time.Duration) *bool {
	cfg := g.cfg.Load()
	if !hasAgentRoutes(cfg, agent) {
		return nil
	}
	g.mu.Lock()
	st := g.authLocked(agent)
	fresh := st.known && g.opts.Now().Sub(st.checked) < maxAge
	if fresh || st.checking {
		v := g.signedInLocked(agent)
		g.mu.Unlock()
		return v
	}
	st.checking = true
	g.mu.Unlock()

	check := g.opts.ClaudeSignedIn
	if agent == config.TypeCodex {
		check = g.opts.CodexSignedIn
	}
	signedIn, ok := check(ctx, cfg.AgentCommand(agent), cfg.SearchPath())

	g.mu.Lock()
	st.checking = false
	st.checked = g.opts.Now()
	g.mu.Unlock()
	if ok {
		g.setSignedIn(agent, signedIn)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.signedInLocked(agent)
}

// setClaudeSignedIn records Claude Code's sign-in state.
func (g *Gateway) setClaudeSignedIn(signedIn bool) { g.setSignedIn(config.TypeClaude, signedIn) }

// setSignedIn records an agent's sign-in state and tells subscribers when it
// changed.
func (g *Gateway) setSignedIn(agent string, signedIn bool) {
	g.mu.Lock()
	st := g.authLocked(agent)
	changed := !st.known || st.signedIn != signedIn
	st.known, st.signedIn = true, signedIn
	g.mu.Unlock()
	if !changed {
		return
	}
	if agent == config.TypeClaude {
		g.broadcast(Event{Type: "claude_auth", Data: map[string]bool{"signed_in": signedIn}})
	} else {
		g.broadcast(Event{Type: "agent_auth", Data: map[string]any{"agent": agent, "signed_in": signedIn}})
	}
	if signedIn {
		g.clearSignedOutNotice(agent)
	} else {
		go g.noticeSignedOut(agent)
	}
}

// authLocked returns the agent's sign-in record. g.mu must be held.
func (g *Gateway) authLocked(agent string) *claudeAuth {
	if g.auth == nil {
		g.auth = map[string]*claudeAuth{}
	}
	st := g.auth[agent]
	if st == nil {
		st = &claudeAuth{}
		g.auth[agent] = st
	}
	return st
}

func (g *Gateway) claudeSignedInLocked() *bool { return g.signedInLocked(config.TypeClaude) }

func (g *Gateway) signedInLocked(agent string) *bool {
	st := g.auth[agent]
	if st == nil || !st.known {
		return nil
	}
	v := st.signedIn
	return &v
}

// signedInMapLocked returns the known sign-in states by agent type.
func (g *Gateway) signedInMapLocked() map[string]bool {
	var out map[string]bool
	for a, st := range g.auth {
		if st.known {
			if out == nil {
				out = map[string]bool{}
			}
			out[a] = st.signedIn
		}
	}
	return out
}
