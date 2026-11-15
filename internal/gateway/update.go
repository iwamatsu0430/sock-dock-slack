package gateway

import (
	"context"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/updates"
)

// A newer SockDock: checked when SockDock starts and
// once a day, told once per version.

const (
	stateKeyUpdateLatest   = "update_latest"
	stateKeyUpdateNotified = "update_notified"

	updateFirstCheck = time.Minute
	updateInterval   = 24 * time.Hour
	updateRetry      = time.Hour
)

// UpdateInfo is a newer release, as Status shows it.
type UpdateInfo struct {
	// Version is without the "v" ("0.2.0"); URL is the release's page.
	Version string `json:"version"`
	URL     string `json:"url"`
	// Notes are the release notes per language ("en", "ja").
	Notes map[string]string `json:"notes,omitempty"`
}

// loadUpdate restores the newer release found before a restart, so the menu
// shows it before the first check.
func (g *Gateway) loadUpdate(ctx context.Context) {
	var rel updates.Release
	if ok, err := g.opts.Store.GetState(ctx, stateKeyUpdateLatest, &rel); err != nil || !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if updates.Newer(rel.Tag, g.opts.Version) {
		g.update = &rel
	}
}

// updateInfoLocked is the newer release, or nil when there is none or checking
// is off.
func (g *Gateway) updateInfoLocked() *UpdateInfo {
	if g.update == nil || !g.cfg.Load().Settings.UpdatesOn() {
		return nil
	}
	return &UpdateInfo{Version: g.update.Version, URL: g.update.URL, Notes: updates.Notes(g.update.Body)}
}

// checkUpdates asks GitHub for the latest release when it is time. Only a
// release build checks (a local build's version is not vX.Y.Z).
func (g *Gateway) checkUpdates(ctx context.Context) {
	if !g.cfg.Load().Settings.UpdatesOn() || !updates.IsRelease(g.opts.Version) {
		return
	}
	g.mu.Lock()
	now := g.opts.Now()
	if g.updateChecking || now.Before(g.updateNext) {
		g.mu.Unlock()
		return
	}
	g.updateChecking = true
	g.mu.Unlock()

	go func() {
		// Checking ends after the newer version has been kept and told.
		defer func() {
			g.mu.Lock()
			g.updateChecking = false
			g.mu.Unlock()
		}()
		rel, err := updates.Latest(ctx, g.opts.UpdateClient, g.opts.UpdateURL, "SockDock/"+g.opts.Version)
		g.mu.Lock()
		if err != nil {
			g.updateNext = g.opts.Now().Add(updateRetry)
			g.mu.Unlock()
			g.log.Info("could not check for a newer version", "err", err)
			return
		}
		g.updateNext = g.opts.Now().Add(updateInterval)
		newer := updates.Newer(rel.Tag, g.opts.Version)
		if newer {
			g.update = &rel
		} else {
			g.update = nil
		}
		g.mu.Unlock()
		if !newer {
			return
		}
		if err := g.opts.Store.SetState(ctx, stateKeyUpdateLatest, rel); err != nil {
			g.log.Warn("could not keep the newer version", "err", err)
		}
		g.announceUpdate(ctx, rel)
	}()
}

// announceUpdate tells the app and the owner about a newer release, once per
// version.
func (g *Gateway) announceUpdate(ctx context.Context, rel updates.Release) {
	var notified string
	if _, err := g.opts.Store.GetState(ctx, stateKeyUpdateNotified, &notified); err == nil && notified == rel.Tag {
		return
	}
	if err := g.opts.Store.SetState(ctx, stateKeyUpdateNotified, rel.Tag); err != nil {
		g.log.Warn("could not keep the notified version", "err", err)
	}
	g.log.Info("a newer version is out", "version", rel.Version)
	g.broadcast(Event{Type: "update_available", Data: UpdateInfo{Version: rel.Version, URL: rel.URL}})

	cfg := g.cfg.Load()
	t := dmText[cfg.Settings.ResolvedLanguage]
	if t == nil {
		t = dmText["en"]
	}
	link := ""
	if rel.URL != "" {
		link = "<" + rel.URL + "|" + t["release_notes"] + ">"
	}
	text := strings.NewReplacer("{version}", rel.Version, "{link}", link).Replace(cfg.Settings.Messages.UpdateAvailable)
	g.notifyOwner(ctx, strings.TrimSpace(text))
}
