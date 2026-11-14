package gateway

import (
	"context"
	"fmt"
	"time"
)

// WatchOptions tune the watchdog.
type WatchOptions struct {
	Interval time.Duration
	// SleepJump is how far the wall clock may run ahead of the monotonic
	// clock before we assume the machine slept and force a reconnect.
	SleepJump time.Duration
	// MaxDisconnected ends the process (so launchd restarts it) after this long
	// without a connection.
	MaxDisconnected time.Duration
}

// DefaultWatchOptions are used by `sockdock run`.
var DefaultWatchOptions = WatchOptions{
	Interval:        15 * time.Second,
	SleepJump:       30 * time.Second,
	MaxDisconnected: 5 * time.Minute,
}

// Watch runs until ctx ends. It calls reconnect after a sleep/wake and
// returns an error when the connection has been down for too long.
func (g *Gateway) Watch(ctx context.Context, o WatchOptions, reconnect func()) error {
	t := time.NewTicker(o.Interval)
	defer t.Stop()
	lastWall, lastMono := time.Now().Round(0), time.Now()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-t.C:
			wall := now.Round(0)
			// On macOS the monotonic clock stops while asleep; the wall clock does not.
			if jump := wall.Sub(lastWall) - now.Sub(lastMono); jump > o.SleepJump {
				g.log.Info("wake from sleep detected; reconnecting", "slept", jump.Round(time.Second))
				reconnect()
			}
			lastWall, lastMono = wall, now

			if ok, since := g.connectedSince(); !ok && time.Since(since) > o.MaxDisconnected {
				return fmt.Errorf("not connected to Slack for %s", time.Since(since).Round(time.Second))
			}
			g.pruneOccasionally(ctx, now)
			g.expireRegistration()
			g.checkClaudeForOwner(ctx)
		}
	}
}

// checkClaudeForOwner asks Claude Code about its sign-in at most once an
// hour while the owner gets DM notices, so a signed-out Claude Code is noticed
// even when only the service runs.
func (g *Gateway) checkClaudeForOwner(ctx context.Context) {
	s := g.cfg.Load().Settings
	if !s.NotifyDM || s.Owner == "" {
		return
	}
	go g.CheckClaude(ctx, time.Hour)
}

func (g *Gateway) pruneOccasionally(ctx context.Context, now time.Time) {
	if now.Sub(g.lastPrune) < time.Hour {
		return
	}
	g.lastPrune = now
	if err := g.opts.Store.Prune(ctx, now.Add(-7*24*time.Hour)); err != nil {
		g.log.Warn("prune failed", "err", err)
	}
}
