package gateway

import (
	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/router"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// HandleReaction is called for every reaction added to or removed from a
// message. A rule whose condition is that emoji starts on the post, as if the
// post had just matched it; with reaction_removed it runs again in the same
// thread when the reaction is gone, and again when it comes back.
func (g *Gateway) HandleReaction(ev event.ReactionEvent) {
	ctx := g.runCtx
	g.mu.Lock()
	self := g.self
	// Remember that reactions from this channel reach SockDock at all (the
	// Slack app may not subscribe to them). DMs count as one channel, "dm".
	if ev.IsIM() {
		g.heardReactions[config.ChannelDM] = true
	} else if ev.Channel != "" {
		g.heardReactions[ev.Channel] = true
	}
	g.mu.Unlock()

	// SockDock's own reactions are dropped before anything else: the 👀 it
	// adds and removes around a run must never start another run, whatever
	// emoji a rule uses.
	if ev.User == "" || ev.User == self.UserID {
		return
	}
	reader, ok := g.opts.API.(slackio.ReactionReader)
	name := config.NormalizeReaction(ev.Name)
	if !ok || name == "" || ev.Channel == "" || ev.TS == "" {
		return
	}
	th, err := g.opts.Store.GetThread(ctx, ev.Channel, ev.TS)
	if err != nil {
		g.log.Error("thread lookup failed", "err", err)
		return
	}
	cfg := g.cfg.Load()

	var r *config.Route
	switch {
	case th == nil && !ev.Removed:
		// A new post for the rules. SockDock does not run for its own posts.
		if ev.ItemUser != "" && ev.ItemUser == self.UserID {
			return
		}
		r = cfg.MatchReaction(ev.Channel, name, config.MatchOptions{
			Skip: g.isPaused, ByUser: true, User: ev.User, IM: ev.IsIM(),
		})
	case th != nil && th.Reaction != "":
		// The post's rule already ran for this reaction. It runs again only
		// when the reaction's state changes, and only with reaction_removed.
		was := config.ReactionAdded
		if !ev.Removed {
			was = config.ReactionRemoved
		}
		if th.Reaction != was {
			return
		}
		r = cfg.Route(th.Route)
		if r == nil || r.Kind() != config.KindReaction || r.Pattern() != name ||
			!r.ReactionRemoved || g.isPaused(r.Name) || !r.Allows(ev.User) {
			return
		}
	}
	if r == nil {
		return
	}

	if ev.Removed {
		// "Removed" means nobody who may run the rule still has the reaction
		// on the post. Other people's reactions, and SockDock's own, do not
		// count.
		users, err := reader.ReactionUsers(ctx, ev.Channel, ev.TS)
		if err != nil {
			g.log.Warn("could not read the post's reactions", "err", err, "channel", ev.Channel, "ts", ev.TS)
			return
		}
		for n, us := range users {
			if config.NormalizeReaction(n) != name {
				continue
			}
			for _, u := range us {
				// Slack may still list the reaction that was just removed.
				if u == self.UserID || (u == ev.User && n == ev.Name) {
					continue
				}
				if r.Allows(u) {
					return
				}
			}
		}
	}

	m, ok, err := reader.Post(ctx, ev.Channel, ev.TS)
	if err != nil {
		g.log.Warn("could not read the post a reaction is on", "err", err, "channel", ev.Channel, "ts", ev.TS)
		return
	}
	// Not a top-level post (a reply in a thread, a system message, deleted),
	// or SockDock's own post.
	if !ok || (self.BotID != "" && m.BotID == self.BotID) || (self.UserID != "" && m.User == self.UserID) {
		return
	}
	if ev.IsIM() {
		m.ChannelType = "im"
	}
	m.Reaction = &event.Reaction{Name: name, User: ev.User, Removed: ev.Removed}

	// The store decides between events that arrive together: one post starts
	// one rule once, and each change of state runs it once.
	if th == nil {
		fresh, err := g.opts.Store.BindThreadIfNew(ctx, store.Thread{
			Channel: ev.Channel, ThreadTS: ev.TS, Route: r.Name, Reaction: config.ReactionAdded,
			Groups: map[string]string{"text": config.PostBody(m.Text, self.UserID)},
		})
		if err != nil {
			g.log.Error("could not bind thread", "err", err)
			return
		}
		if !fresh {
			return
		}
		g.log.Info("thread started by a reaction", "route", r.Name, "channel", ev.Channel, "ts", ev.TS, "reaction", name)
	} else {
		from, to := config.ReactionRemoved, config.ReactionAdded
		if ev.Removed {
			from, to = to, from
		}
		moved, err := g.opts.Store.SetThreadReaction(ctx, ev.Channel, ev.TS, from, to)
		if err != nil {
			g.log.Error("could not record the reaction", "err", err)
			return
		}
		if !moved {
			return
		}
	}
	g.mu.Lock()
	g.conn.lastEvent = g.opts.Now()
	g.mu.Unlock()
	g.disp.Submit(router.ThreadKey(ev.Channel, ev.TS), m, config.IsAgent(r.Handler.Type))
	g.broadcast(Event{Type: "queued", Data: map[string]string{"channel": ev.Channel, "thread_ts": ev.TS}})
}
