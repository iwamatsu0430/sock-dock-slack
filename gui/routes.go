package main

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/slack-go/slack"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// The form types are shared with the control API (internal/rules).
type (
	RouteForm    = rules.Form
	FieldProblem = rules.FieldProblem
	Condition    = rules.Condition
)

// RouteSaveResult is the outcome of saving a route.
type RouteSaveResult struct {
	OK       bool           `json:"ok"`
	Problems []FieldProblem `json:"problems,omitempty"`
	// ConfigProblems are problems found in the whole config (rare: e.g. an
	// unrelated route that no longer validates).
	ConfigProblems []string `json:"config_problems,omitempty"`
	// ClaudeCommand is set when the app filled in settings.claude_command.
	ClaudeCommand string `json:"claude_command,omitempty"`
	// CodexCommand is set when the app filled in settings.codex_command.
	CodexCommand string `json:"codex_command,omitempty"`
}

// ensureConfig writes a minimal config when there is none, so the gateway
// can start (with no routes) and the app can add routes.
func (a *AppService) ensureConfig() error {
	_, _, err := rules.Read(a.dirs.ConfigFile)
	return err
}

func (a *AppService) readRaw() ([]byte, *config.Config, error) {
	return rules.Read(a.dirs.ConfigFile)
}

// write saves new config content and turns the result into a RouteSaveResult.
func (a *AppService) write(content []byte) (RouteSaveResult, error) {
	saved, err := a.saveConfig(string(content))
	if err != nil {
		return RouteSaveResult{}, err
	}
	return RouteSaveResult{OK: saved.OK, ConfigProblems: saved.Problems}, nil
}

// ValidateRoute checks a form without saving it.
func (a *AppService) ValidateRoute(f RouteForm) []FieldProblem {
	_, raw, err := a.readRaw()
	if err != nil {
		return []FieldProblem{{Field: "config", Code: "config_broken", Detail: err.Error()}}
	}
	return rules.Validate(f, raw)
}

// SaveRoute adds or updates a route and saves the config (the gateway reloads).
func (a *AppService) SaveRoute(f RouteForm) (RouteSaveResult, error) {
	data, raw, err := a.readRaw()
	if err != nil {
		return RouteSaveResult{ConfigProblems: []string{err.Error()}}, nil
	}
	saved, err := rules.Save(data, raw, f)
	if err != nil {
		return RouteSaveResult{}, err
	}
	if len(saved.Problems) > 0 {
		return RouteSaveResult{Problems: saved.Problems}, nil
	}
	res, err := a.write(saved.Content)
	res.ClaudeCommand, res.CodexCommand = saved.ClaudeCommand, saved.CodexCommand
	return res, err
}

// DeleteRoute removes a route and saves the config.
func (a *AppService) DeleteRoute(name string) (RouteSaveResult, error) {
	data, raw, err := a.readRaw()
	if err != nil {
		return RouteSaveResult{ConfigProblems: []string{err.Error()}}, nil
	}
	content, err := rules.Delete(data, raw, name)
	if err != nil {
		return RouteSaveResult{}, err
	}
	return a.write(content)
}

// ReorderRules saves the rules in the given order (all rule names). Within a
// channel, rules are tried top to bottom.
func (a *AppService) ReorderRules(names []string) (RouteSaveResult, error) {
	data, raw, err := a.readRaw()
	if err != nil {
		return RouteSaveResult{ConfigProblems: []string{err.Error()}}, nil
	}
	content, err := rules.Reorder(data, raw, names)
	if err != nil {
		return RouteSaveResult{}, err
	}
	return a.write(content)
}

// Channel is a Slack channel the bot is in.
type Channel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Private bool   `json:"private"`
}

// ChannelList is the channels the bot can see, or why it could not list them.
type ChannelList struct {
	Channels []Channel `json:"channels"`
	// Problem is "no_token", "missing_scope" or "error" (see Error).
	Problem string `json:"problem,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ListChannels returns the channels the bot is a member of (needs the
// channels:read and groups:read scopes).
func (a *AppService) ListChannels() ChannelList {
	bot, _, _ := secret.Lookup(secret.BotToken, a.secretsFile())
	if bot == "" {
		return ChannelList{Channels: []Channel{}, Problem: "no_token"}
	}
	c, cancel := ctx()
	defer cancel()
	api := slack.New(bot)
	out := []Channel{}
	cursor := ""
	for range 10 {
		chs, next, err := api.GetConversationsForUserContext(c, &slack.GetConversationsForUserParameters{
			Types: []string{"public_channel", "private_channel"}, ExcludeArchived: true, Limit: 200, Cursor: cursor,
		})
		if err != nil {
			problem := "error"
			if strings.Contains(err.Error(), "missing_scope") {
				problem = "missing_scope"
			}
			return ChannelList{Channels: out, Problem: problem, Error: err.Error()}
		}
		for _, ch := range chs {
			out = append(out, Channel{ID: ch.ID, Name: ch.Name, Private: ch.IsPrivate})
		}
		if next == "" {
			break
		}
		cursor = next
	}
	slices.SortFunc(out, func(x, y Channel) int { return strings.Compare(x.Name, y.Name) })
	a.chMu.Lock()
	for _, ch := range out {
		a.channels[ch.ID] = ch
	}
	a.chMu.Unlock()
	return ChannelList{Channels: out}
}

// ChannelName returns "#name" (or "🔒name") for a channel ID or link, or ""
// when it cannot be looked up (conversations.info needs channels:read).
func (a *AppService) ChannelName(idOrLink string) string {
	id := rules.NormalizeChannel(idOrLink)
	if !rules.IsChannelID(id) {
		return ""
	}
	a.chMu.Lock()
	ch, ok := a.channels[id]
	a.chMu.Unlock()
	if !ok {
		ch = Channel{ID: id}
		if bot, _, _ := secret.Lookup(secret.BotToken, a.secretsFile()); bot != "" {
			c, cancel := ctx()
			info, err := slack.New(bot).GetConversationInfoContext(c, &slack.GetConversationInfoInput{ChannelID: id})
			cancel()
			if err == nil {
				ch = Channel{ID: id, Name: info.Name, Private: info.IsPrivate}
			}
		}
		a.chMu.Lock()
		a.channels[id] = ch
		a.chMu.Unlock()
	}
	if ch.Name == "" {
		return ""
	}
	if ch.Private {
		return "🔒" + ch.Name
	}
	return "#" + ch.Name
}

// channelLabel is "#name" when the channel's name is known, else the ID.
func (a *AppService) channelLabel(id string) string {
	if id == config.ChannelDM {
		return "DM"
	}
	if ch := a.ChannelName(id); ch != "" {
		return ch
	}
	return id
}

// ruleLabel is how menus and notifications name a rule: #channel「keyword」,
// #channel /regex/, or #channel for all posts.
func (a *AppService) ruleLabel(r *config.Route) string {
	ch := a.channelLabel(r.Channel)
	switch r.Kind() {
	case config.KindStartsWith, config.KindContains:
		return a.tr("label.keyword", ch, r.Pattern())
	case config.KindMatch:
		return a.tr("label.regex", ch, r.Match)
	}
	return ch
}

// labelFor returns the label of the rule with this config name.
func (a *AppService) labelFor(name string) string {
	_, raw, err := a.readRaw()
	if err == nil {
		for _, r := range raw.Routes {
			if r.Name == name {
				return a.ruleLabel(r)
			}
		}
	}
	return name
}

// RuleRow is one line of the rule list.
type RuleRow struct {
	rules.Row
	Label string `json:"label"`
	// Channel is the channel's name ("#name") or ID; the list groups by it.
	Channel string `json:"channel"`
	// Last is the most recent run.
	Last *store.Turn `json:"last,omitempty"`
	// Users are the people in Form.AllowedUsers, in order (Name is "" when
	// Slack does not know the ID or users:read is missing).
	Users []slackio.Member `json:"users"`
}

// RuleList is the rule list, or why the config could not be read.
type RuleList struct {
	Rules []RuleRow `json:"rules"`
	Error string    `json:"error,omitempty"`
}

// ListRules returns the rules with their state and last run.
func (a *AppService) ListRules() RuleList {
	_, raw, err := a.readRaw()
	if err != nil {
		return RuleList{Rules: []RuleRow{}, Error: err.Error()}
	}
	paused := map[string]bool{}
	if snap := a.GetSnapshot(); snap.Status != nil {
		for _, r := range snap.Status.Routes {
			paused[r.Name] = r.Paused
		}
	}
	last := map[string]*store.Turn{}
	for _, t := range a.RecentTurns(200) {
		if _, ok := last[t.Route]; !ok {
			last[t.Route] = &t
		}
	}
	var ids []string
	for _, r := range raw.Routes {
		ids = append(ids, r.AllowedUsers...)
	}
	c, cancel := ctx()
	people := a.members.Names(c, ids)
	cancel()
	out := make([]RuleRow, 0, len(raw.Routes))
	for i, row := range rules.List(raw, func(n string) bool { return paused[n] }) {
		r := raw.Routes[i]
		users := []slackio.Member{}
		for _, id := range r.AllowedUsers {
			m, ok := people[id]
			if !ok {
				m = slackio.Member{ID: id}
			}
			users = append(users, m)
		}
		out = append(out, RuleRow{Row: row, Label: a.ruleLabel(r), Channel: a.channelLabel(r.Channel), Last: last[r.Name], Users: users})
	}
	return RuleList{Rules: out}
}

// MemberList is people matching a search, or why they cannot be listed.
type MemberList struct {
	Members []slackio.Member `json:"members"`
	// Problem is "no_token", "missing_scope" (users:read) or "error" (see Error).
	Problem string `json:"problem,omitempty"`
	Error   string `json:"error,omitempty"`
}

// fetchMembers lists the workspace's people with the Bot Token (users:read).
func (a *AppService) fetchMembers(c context.Context) ([]slackio.Member, error) {
	bot, _, _ := secret.Lookup(secret.BotToken, a.secretsFile())
	if bot == "" {
		return nil, errNoToken
	}
	return slackio.FetchMembers(c, slack.New(bot))
}

var errNoToken = errors.New("no Bot Token")

// ListMembers searches the workspace's people by name for the "実行できる人"
// field (at most 8). An empty query only checks that names can be listed.
func (a *AppService) ListMembers(query string) MemberList {
	c, cancel := ctx()
	defer cancel()
	found, err := a.members.Search(c, query, 8)
	switch {
	case errors.Is(err, errNoToken):
		return MemberList{Members: []slackio.Member{}, Problem: "no_token"}
	case slackio.IsMissingScope(err):
		return MemberList{Members: []slackio.Member{}, Problem: "missing_scope"}
	case err != nil:
		return MemberList{Members: []slackio.Member{}, Problem: "error", Error: err.Error()}
	}
	if strings.TrimSpace(query) == "" {
		found = []slackio.Member{}
	}
	return MemberList{Members: found}
}

// PickFolder shows a folder picker and returns the chosen path ("" if cancelled).
func (a *AppService) PickFolder(title string) string {
	d := a.app.Dialog.OpenFile().CanChooseDirectories(true).CanChooseFiles(false).CanCreateDirectories(true).SetTitle(title)
	if a.main != nil {
		d = d.AttachToWindow(a.main)
	}
	p, err := d.PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return rules.TildePath(p)
}

// PickFile shows a file picker and returns the chosen path ("" if cancelled).
func (a *AppService) PickFile(title string) string {
	d := a.app.Dialog.OpenFile().CanChooseFiles(true).CanChooseDirectories(false).SetTitle(title)
	if a.main != nil {
		d = d.AttachToWindow(a.main)
	}
	p, err := d.PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return rules.TildePath(p)
}
