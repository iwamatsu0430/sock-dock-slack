package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/slack-go/slack"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// RouteForm is a route as the app's form edits it: plain choices instead of
// raw YAML. It maps to and from config.Route.
type RouteForm struct {
	// OriginalName is the name before editing ("" for a new route).
	OriginalName string `json:"original_name"`
	// Name is the route's name in the config. The app does not show it; a
	// new route gets one automatically.
	Name string `json:"name"`
	// Channel is a channel ID or a pasted channel link.
	Channel string `json:"channel"`
	// Trigger is the condition kind: "starts_with", "contains", "match" (a
	// regex) or "all_posts". Keyword is the keyword or the regex.
	Trigger string `json:"trigger"`
	Keyword string `json:"keyword"`
	// Type is "claude" or "command".
	Type string `json:"type"`
	// Folder is the working directory (claude: the project folder).
	Folder string `json:"folder"`

	// claude
	Instructions string   `json:"instructions"`
	AllowWeb     bool     `json:"allow_web"`
	AllowEdit    bool     `json:"allow_edit"`
	Commands     []string `json:"commands"`    // each allows "Bash(<command> *)"
	OtherTools   []string `json:"other_tools"` // other allowed_tools rules, kept as is
	Model        string   `json:"model"`
	PromptFile   string   `json:"prompt_file"`

	// command
	Program string `json:"program"`
	Args    string `json:"args"`

	TimeoutSec int `json:"timeout_sec"`
}

// FieldProblem is a validation problem tied to a form field. Code is
// translated by the UI; Detail carries extra text (e.g. a path).
type FieldProblem struct {
	Field  string `json:"field"`
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// RouteSaveResult is the outcome of saving a route.
type RouteSaveResult struct {
	OK       bool           `json:"ok"`
	Problems []FieldProblem `json:"problems,omitempty"`
	// ConfigProblems are problems found in the whole config (rare: e.g. an
	// unrelated route that no longer validates).
	ConfigProblems []string `json:"config_problems,omitempty"`
	// ClaudeCommand is set when the app filled in settings.claude_command.
	ClaudeCommand string `json:"claude_command,omitempty"`
}

var (
	webTools  = []string{"WebSearch", "WebFetch"}
	editTools = []string{"Edit", "Write"}
	bashRule  = regexp.MustCompile(`^Bash\((.+?) \*\)$`)
	channelID = regexp.MustCompile(`^[CGD][A-Z0-9]{6,}$`)
	linkID    = regexp.MustCompile(`/archives/([CGD][A-Z0-9]{6,})`)
	nameOK    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// ensureConfig writes a minimal config when there is none, so the gateway
// can start (with no routes) and the app can add routes.
func (a *AppService) ensureConfig() error {
	if _, err := os.Stat(a.dirs.ConfigFile); err == nil || !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.dirs.ConfigFile), 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.dirs.ConfigFile, []byte(config.MinimalConfig), 0o600)
}

func (a *AppService) readRaw() ([]byte, *config.Config, error) {
	if err := a.ensureConfig(); err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(a.dirs.ConfigFile)
	if err != nil {
		return nil, nil, err
	}
	raw, err := config.ParseRaw(data)
	if err != nil {
		return data, nil, err
	}
	return data, raw, nil
}

// ListRoutes returns the configured routes as forms.
func (a *AppService) ListRoutes() ([]RouteForm, error) {
	_, raw, err := a.readRaw()
	if err != nil {
		return nil, err
	}
	out := make([]RouteForm, 0, len(raw.Routes))
	for _, r := range raw.Routes {
		out = append(out, routeToForm(r))
	}
	return out, nil
}

func routeToForm(r *config.Route) RouteForm {
	h := r.Handler
	f := RouteForm{
		OriginalName: r.Name, Name: r.Name, Channel: r.Channel, Type: h.Type, Folder: h.Cwd,
		Instructions: h.Instructions, Model: h.Model, PromptFile: h.Prompt, TimeoutSec: h.TimeoutSec,
		Commands: []string{}, OtherTools: []string{},
	}
	f.Trigger, f.Keyword = r.Kind(), r.Pattern()
	if f.Trigger == "" {
		f.Trigger = config.KindStartsWith
	}
	if h.Type == config.TypeCommand && len(h.Command) > 0 {
		f.Program = h.Command[0]
		f.Args = joinArgs(h.Command[1:])
	}
	has := func(names []string) bool {
		for _, n := range names {
			if !slices.Contains(h.AllowedTools, n) {
				return false
			}
		}
		return true
	}
	f.AllowWeb, f.AllowEdit = has(webTools), has(editTools)
	for _, rule := range h.AllowedTools {
		switch {
		case f.AllowWeb && slices.Contains(webTools, rule), f.AllowEdit && slices.Contains(editTools, rule):
		case bashRule.MatchString(rule):
			f.Commands = append(f.Commands, bashRule.FindStringSubmatch(rule)[1])
		default:
			f.OtherTools = append(f.OtherTools, rule)
		}
	}
	return f
}

// formToRoute builds the raw route that will be written to the config.
func formToRoute(f RouteForm, base *config.Route) *config.Route {
	r := &config.Route{}
	if base != nil {
		// Keep fields the form does not edit (env_file, env, tools, extra_args, …).
		copyRoute := *base
		r = &copyRoute
	}
	r.Name = strings.TrimSpace(f.Name)
	r.Channel = normalizeChannel(f.Channel)
	r.StartsWith, r.Contains, r.Match, r.AllPosts = "", "", "", false
	switch f.Trigger {
	case config.KindStartsWith:
		r.StartsWith = strings.TrimSpace(f.Keyword)
	case config.KindContains:
		r.Contains = strings.TrimSpace(f.Keyword)
	case config.KindMatch:
		r.Match = strings.TrimSpace(f.Keyword)
	case config.KindAllPosts:
		r.AllPosts = true
	}
	h := &r.Handler
	h.Type = f.Type
	h.Cwd = tildePath(strings.TrimSpace(f.Folder))
	h.TimeoutSec = f.TimeoutSec
	switch f.Type {
	case config.TypeClaude:
		h.Command = nil
		h.Instructions = strings.TrimSpace(f.Instructions)
		h.Prompt = strings.TrimSpace(f.PromptFile)
		h.Model = strings.TrimSpace(f.Model)
		var tools []string
		if f.AllowWeb {
			tools = append(tools, webTools...)
		}
		if f.AllowEdit {
			tools = append(tools, editTools...)
		}
		for _, c := range f.Commands {
			if c = strings.TrimSpace(c); c != "" {
				tools = append(tools, "Bash("+c+" *)")
			}
		}
		for _, t := range f.OtherTools {
			if t = strings.TrimSpace(t); t != "" && !slices.Contains(tools, t) {
				tools = append(tools, t)
			}
		}
		h.AllowedTools = tools
	case config.TypeCommand:
		h.Instructions, h.Prompt, h.Model, h.AllowedTools = "", "", "", nil
		h.Command = append([]string{tildePath(strings.TrimSpace(f.Program))}, splitArgs(f.Args)...)
	}
	return r
}

// ValidateRoute checks a form without saving it.
func (a *AppService) ValidateRoute(f RouteForm) []FieldProblem {
	_, raw, err := a.readRaw()
	if err != nil {
		return []FieldProblem{{Field: "config", Code: "config_broken", Detail: err.Error()}}
	}
	return a.validateForm(f, raw)
}

func (a *AppService) validateForm(f RouteForm, raw *config.Config) []FieldProblem {
	var ps []FieldProblem
	add := func(field, code, detail string) {
		ps = append(ps, FieldProblem{Field: field, Code: code, Detail: detail})
	}

	if name := strings.TrimSpace(f.Name); name != "" {
		switch {
		case !nameOK.MatchString(name):
			add("name", "bad_name", "")
		default:
			for _, r := range raw.Routes {
				if r.Name == name && r.Name != f.OriginalName {
					add("name", "duplicate", "")
				}
			}
		}
	}

	if ch := normalizeChannel(f.Channel); ch == "" {
		add("channel", "required", "")
	} else if !channelID.MatchString(ch) && !strings.Contains(ch, "${") {
		add("channel", "bad_channel", "")
	}

	switch kw := strings.TrimSpace(f.Keyword); f.Trigger {
	case config.KindStartsWith, config.KindContains:
		if kw == "" {
			add("keyword", "required", "")
		}
	case config.KindMatch:
		if kw == "" {
			add("keyword", "required", "")
		} else if _, err := regexp.Compile(kw); err != nil {
			add("keyword", "bad_regex", err.Error())
		}
	case config.KindAllPosts:
	default:
		add("trigger", "required", "")
	}

	folder := paths.ExpandHome(strings.TrimSpace(f.Folder))
	if folder == "" {
		add("folder", "required", "")
	} else if st, err := os.Stat(folder); err != nil || !st.IsDir() {
		add("folder", "not_dir", folder)
	}

	switch f.Type {
	case config.TypeClaude:
		if p := strings.TrimSpace(f.PromptFile); p != "" && folder != "" {
			pp := paths.ExpandHome(p)
			if !filepath.IsAbs(pp) {
				pp = filepath.Join(folder, pp)
			}
			if _, err := os.Stat(pp); err != nil {
				add("prompt_file", "not_found", pp)
			}
		}
	case config.TypeCommand:
		prog := paths.ExpandHome(strings.TrimSpace(f.Program))
		if prog == "" {
			add("program", "required", "")
			break
		}
		if !strings.ContainsRune(prog, '/') {
			if _, err := config.LookPath(prog, os.Getenv("PATH")); err != nil {
				add("program", "not_found", prog)
			}
			break
		}
		if !filepath.IsAbs(prog) && folder != "" {
			prog = filepath.Join(folder, prog)
		}
		if st, err := os.Stat(prog); err != nil {
			add("program", "not_found", prog)
		} else if st.IsDir() || st.Mode()&0o111 == 0 {
			add("program", "not_executable", prog)
		}
	default:
		add("type", "required", "")
	}
	return ps
}

// SaveRoute adds or updates a route and saves the config (the gateway reloads).
func (a *AppService) SaveRoute(f RouteForm) (RouteSaveResult, error) {
	data, raw, err := a.readRaw()
	if err != nil {
		return RouteSaveResult{ConfigProblems: []string{err.Error()}}, nil
	}
	if strings.TrimSpace(f.Name) == "" {
		f.Name = autoName(raw)
	}
	if ps := a.validateForm(f, raw); len(ps) > 0 {
		return RouteSaveResult{Problems: ps}, nil
	}

	// A new rule, or one moved to another channel, goes to the bottom of its
	// channel (rules are tried top to bottom).
	var routes []*config.Route
	var moved *config.Route
	replaced := false
	for _, r := range raw.Routes {
		if f.OriginalName != "" && r.Name == f.OriginalName {
			nr := formToRoute(f, r)
			replaced = true
			if nr.Channel != r.Channel {
				moved = nr
				continue
			}
			routes = append(routes, nr)
		} else {
			routes = append(routes, r)
		}
	}
	switch {
	case moved != nil:
		routes = append(routes, moved)
	case !replaced:
		routes = append(routes, formToRoute(f, nil))
	}
	content, err := config.ReplaceRoutes(data, routes)
	if err != nil {
		return RouteSaveResult{}, err
	}

	res := RouteSaveResult{}
	// Claude Code: if the configured command cannot be found (or is a shim),
	// fill in an absolute path the app found.
	if f.Type == config.TypeClaude {
		if path, changed := a.claudeCommandFix(raw); changed {
			if content, err = config.SetSetting(content, "claude_command", path); err != nil {
				return RouteSaveResult{}, err
			}
			res.ClaudeCommand = path
		} else if path == "" {
			return RouteSaveResult{Problems: []FieldProblem{{Field: "type", Code: "claude_not_found"}}}, nil
		}
	}

	saved, err := a.saveConfig(string(content))
	if err != nil {
		return RouteSaveResult{}, err
	}
	if !saved.OK {
		res.ConfigProblems = saved.Problems
		return res, nil
	}
	res.OK = true
	return res, nil
}

// claudeCommandFix returns a usable Claude Code path. changed is true when the
// config should be updated to it; path is "" when none was found.
func (a *AppService) claudeCommandFix(raw *config.Config) (path string, changed bool) {
	cur := []string(raw.Settings.ClaudeCommand)
	if len(cur) > 0 && cur[0] != "" && cur[0] != "claude" {
		p := paths.ExpandHome(cur[0])
		if st, err := os.Stat(p); err == nil && !st.IsDir() && !strings.Contains(p, "/shims/") {
			return p, false
		}
	}
	found := detectClaude()
	if len(found) == 0 {
		return "", false
	}
	return found[0], true
}

// DeleteRoute removes a route and saves the config.
func (a *AppService) DeleteRoute(name string) (RouteSaveResult, error) {
	data, raw, err := a.readRaw()
	if err != nil {
		return RouteSaveResult{ConfigProblems: []string{err.Error()}}, nil
	}
	routes := slices.DeleteFunc(slices.Clone(raw.Routes), func(r *config.Route) bool { return r.Name == name })
	content, err := config.ReplaceRoutes(data, routes)
	if err != nil {
		return RouteSaveResult{}, err
	}
	saved, err := a.saveConfig(string(content))
	if err != nil {
		return RouteSaveResult{}, err
	}
	return RouteSaveResult{OK: saved.OK, ConfigProblems: saved.Problems}, nil
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
	id := normalizeChannel(idOrLink)
	if !channelID.MatchString(id) {
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
	Form  RouteForm `json:"form"`
	Label string    `json:"label"`
	// Channel is the channel's name ("#name") or ID; the list groups by it.
	Channel string `json:"channel"`
	// ShadowedBy is the condition of an earlier rule in the same channel that
	// every matching post meets first, so this rule never runs.
	ShadowedBy *Condition `json:"shadowed_by,omitempty"`
	// Off is true when the rule is turned off (paused).
	Off bool `json:"off"`
	// Problem is why the rule cannot run, if anything.
	Problem *FieldProblem `json:"problem,omitempty"`
	// Last is the most recent run.
	Last *store.Turn `json:"last,omitempty"`
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
	snap := a.GetSnapshot()
	if snap.Status != nil {
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
	out := make([]RuleRow, 0, len(raw.Routes))
	for i, r := range raw.Routes {
		f := routeToForm(r)
		row := RuleRow{Form: f, Label: a.ruleLabel(r), Channel: a.channelLabel(r.Channel), Off: paused[r.Name], Last: last[r.Name]}
		for _, p := range a.validateForm(f, raw) {
			if p.Field != "name" {
				row.Problem = &p
				break
			}
		}
		if row.Problem == nil && !row.Off {
			for j, e := range raw.Routes[:i] {
				if !out[j].Off && out[j].Problem == nil && config.ShadowedBy(e, r) {
					row.ShadowedBy = &Condition{Trigger: e.Kind(), Keyword: e.Pattern()}
					break
				}
			}
		}
		out = append(out, row)
	}
	return RuleList{Rules: out}
}

// Condition is a rule's condition as the list shows it.
type Condition struct {
	Trigger string `json:"trigger"`
	Keyword string `json:"keyword"`
}

// ReorderRules saves the rules in the given order (all rule names). Within a
// channel, rules are tried top to bottom.
func (a *AppService) ReorderRules(names []string) (RouteSaveResult, error) {
	data, raw, err := a.readRaw()
	if err != nil {
		return RouteSaveResult{ConfigProblems: []string{err.Error()}}, nil
	}
	byName := map[string]*config.Route{}
	for _, r := range raw.Routes {
		byName[r.Name] = r
	}
	if len(names) != len(raw.Routes) {
		return RouteSaveResult{}, errors.New("the rules changed; reload the list")
	}
	routes := make([]*config.Route, 0, len(names))
	for _, n := range names {
		r, ok := byName[n]
		if !ok {
			return RouteSaveResult{}, errors.New("the rules changed; reload the list")
		}
		delete(byName, n)
		routes = append(routes, r)
	}
	content, err := config.ReplaceRoutes(data, routes)
	if err != nil {
		return RouteSaveResult{}, err
	}
	saved, err := a.saveConfig(string(content))
	if err != nil {
		return RouteSaveResult{}, err
	}
	return RouteSaveResult{OK: saved.OK, ConfigProblems: saved.Problems}, nil
}

// autoName returns an unused route name ("rule-1", "rule-2", …).
func autoName(raw *config.Config) string {
	used := map[string]bool{}
	for _, r := range raw.Routes {
		used[r.Name] = true
	}
	for i := 1; ; i++ {
		if n := fmt.Sprintf("rule-%d", i); !used[n] {
			return n
		}
	}
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
	return tildePath(p)
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
	return tildePath(p)
}

// normalizeChannel accepts an ID, "#id" or a channel link and returns the ID.
func normalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	if m := linkID.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return strings.TrimPrefix(s, "#")
}

// tildePath shortens a path under the home directory to ~/… for readability.
func tildePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || p == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(os.PathSeparator)); ok {
		return "~/" + rest
	}
	return p
}

// splitArgs splits a command line on spaces, honoring double and single quotes.
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inArg {
				out = append(out, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		out = append(out, cur.String())
	}
	return out
}

func joinArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			parts[i] = fmt.Sprintf("%q", a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
