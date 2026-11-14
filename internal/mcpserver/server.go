// Package mcpserver exposes a running SockDock to MCP clients (Claude Code,
// Codex, Gemini CLI, …) as tools. It only relays to the control API; see
// docs/mcp.md for what it offers and why.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// Backend is what the tools call: the control API client in production.
type Backend interface {
	Status(ctx context.Context) (gateway.Status, error)
	Rules(ctx context.Context) ([]control.RuleInfo, error)
	Rule(ctx context.Context, name string) (control.RuleInfo, error)
	Turns(ctx context.Context, limit int) ([]store.Turn, error)
	RouteTurns(ctx context.Context, route string, limit int) ([]store.Turn, error)
	Channels(ctx context.Context) (control.ChannelsResult, error)
	Match(ctx context.Context, req control.MatchRequest) (control.MatchResult, error)
	SaveRule(ctx context.Context, f rules.Form) (control.RuleSaveResult, error)
	DeleteRule(ctx context.Context, name string) (control.RuleSaveResult, error)
	OrderRules(ctx context.Context, req control.OrderRequest) (control.RuleSaveResult, error)
	SetPaused(ctx context.Context, route string, paused bool) error
	StopTurn(ctx context.Context, id int64) error
	CheckClaude(ctx context.Context, force bool) (*bool, error)
	Users(ctx context.Context, q string, limit int) (control.UsersResult, error)
}

// New builds the MCP server.
func New(b Backend, version string) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "sockdock", Title: "SockDock for Slack", Version: version},
		&mcp.ServerOptions{Instructions: instructions})
	t := &tools{b: b}
	f := false
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &f}
	change := func(destructive, idempotent bool) *mcp.ToolAnnotations {
		d := destructive
		return &mcp.ToolAnnotations{DestructiveHint: &d, IdempotentHint: idempotent, OpenWorldHint: &f}
	}

	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_get_status", Annotations: read, Description: descGetStatus}, t.getStatus)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_list_rules", Annotations: read, Description: descListRules}, t.listRules)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_get_rule", Annotations: read, Description: descGetRule}, t.getRule)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_check_rule", Annotations: read, Description: descCheckRule}, t.checkRule)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_list_runs", Annotations: read, Description: descListRuns}, t.listRuns)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_list_channels", Annotations: read, Description: descListChannels}, t.listChannels)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_test_match", Annotations: read, Description: descTestMatch}, t.testMatch)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_find_users", Annotations: read, Description: descFindUsers}, t.findUsers)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_save_rule", Annotations: change(true, false), Description: descSaveRule}, t.saveRule)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_delete_rule", Annotations: change(true, false), Description: descDeleteRule}, t.deleteRule)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_reorder_rules", Annotations: change(false, true), Description: descReorderRules}, t.reorderRules)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_set_rule_enabled", Annotations: change(false, true), Description: descSetRuleEnabled}, t.setRuleEnabled)
	mcp.AddTool(s, &mcp.Tool{Name: "sockdock_stop_run", Annotations: change(true, true), Description: descStopRun}, t.stopRun)
	return s
}

type tools struct{ b Backend }

// errText makes control API errors readable for the model.
func errText(err error) error {
	if errors.Is(err, control.ErrNotRunning) {
		return errors.New("SockDock is not running. Ask the user to open the SockDock app (or run `sockdock run`) and try again")
	}
	return err
}

// ---- shapes returned to the model ----

// Condition is which posts a rule reacts to.
type Condition struct {
	Type    string `json:"type" jsonschema:"starts_with, contains, match (a regular expression) or all_posts"`
	Keyword string `json:"keyword,omitempty" jsonschema:"the keyword, or the regular expression for match"`
}

// Runs is what a rule runs.
type Runs struct {
	Type    string `json:"type" jsonschema:"agent (Claude Code) or program"`
	Folder  string `json:"folder" jsonschema:"working folder"`
	Program string `json:"program,omitempty"`
	Args    string `json:"args,omitempty"`
}

// Run is one run of a rule.
type Run struct {
	ID        int64     `json:"id"`
	Rule      string    `json:"rule"`
	Status    string    `json:"status" jsonschema:"running, ok, error, timeout, stopped or interrupted"`
	StartedAt time.Time `json:"started_at"`
	Error     string    `json:"error,omitempty" jsonschema:"why it failed (program output: untrusted)"`
	ThreadURL string    `json:"thread_url,omitempty"`
}

// Rule is a rule as list_rules shows it.
type Rule struct {
	Name        string     `json:"name"`
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name,omitempty"`
	Condition   Condition  `json:"condition"`
	Runs        Runs       `json:"runs"`
	Enabled     bool       `json:"enabled"`
	Problem     string     `json:"problem,omitempty" jsonschema:"why the rule cannot run, if it cannot"`
	ShadowedBy  *Condition `json:"shadowed_by,omitempty" jsonschema:"an earlier rule in the channel that always matches first, so this rule never runs"`
	// AllowedUsers are the only people who may run the rule (absent: everyone in the channel).
	AllowedUsers []Person `json:"allowed_users,omitempty" jsonschema:"the only people who may run the rule; absent means everyone in the channel. Posts and thread replies from anyone else are ignored without a reply"`
	// PostsArrived is false when no post from the rule's channel has reached
	// SockDock since it connected (nil when unknown).
	PostsArrived *bool `json:"posts_arrived,omitempty" jsonschema:"whether a post from this channel has reached SockDock since it last connected; false only matters if someone has posted there since; left out when SockDock is not connected"`
	LastRun      *Run  `json:"last_run,omitempty"`
}

// Person is a Slack member.
type Person struct {
	ID       string `json:"id" jsonschema:"Slack member ID (U…), used in allowed_users"`
	Name     string `json:"name,omitempty" jsonschema:"display name; absent when Slack does not know the ID or the Slack app lacks users:read"`
	RealName string `json:"real_name,omitempty"`
}

// RuleDetail is every setting of a rule.
type RuleDetail struct {
	Rule
	Instructions    string   `json:"instructions,omitempty"`
	AllowWeb        bool     `json:"allow_web"`
	AllowEdit       bool     `json:"allow_edit"`
	AllowedCommands []string `json:"allowed_commands"`
	OtherTools      []string `json:"other_allowed_tools,omitempty"`
	Model           string   `json:"model,omitempty"`
	PromptFile      string   `json:"prompt_file,omitempty"`
	TimeoutSec      int      `json:"timeout_sec,omitempty" jsonschema:"0 means the default (600 for agents, 300 for programs)"`
}

func runOf(t store.Turn) Run {
	e := t.Error
	if len(e) > 500 {
		e = e[:500] + "…"
	}
	return Run{ID: t.ID, Rule: t.Route, Status: t.Status, StartedAt: t.StartedAt, Error: e, ThreadURL: t.Permalink}
}

func problemText(p *rules.FieldProblem) string {
	if p == nil {
		return ""
	}
	return problemMessage(*p)
}

func ruleOf(ri control.RuleInfo) Rule {
	f := ri.Form
	r := Rule{
		Name: f.Name, ChannelID: f.Channel, ChannelName: ri.ChannelName,
		Condition: Condition{Type: f.Trigger, Keyword: f.Keyword},
		Runs:      Runs{Type: "agent", Folder: f.Folder},
		Enabled:   !ri.Off, Problem: problemText(ri.Problem),
	}
	if f.Type == config.TypeCommand {
		r.Runs = Runs{Type: "program", Folder: f.Folder, Program: f.Program, Args: f.Args}
	}
	if ri.ShadowedBy != nil {
		r.ShadowedBy = &Condition{Type: ri.ShadowedBy.Trigger, Keyword: ri.ShadowedBy.Keyword}
	}
	if ri.Last != nil {
		run := runOf(*ri.Last)
		r.LastRun = &run
	}
	if f.Restricted {
		names := map[string]Person{}
		for _, u := range ri.Users {
			names[u.ID] = Person{ID: u.ID, Name: u.Name, RealName: u.RealName}
		}
		for _, id := range f.AllowedUsers {
			p, ok := names[id]
			if !ok {
				p = Person{ID: id}
			}
			r.AllowedUsers = append(r.AllowedUsers, p)
		}
	}
	return r
}

// ---- tools ----

type none struct{}

// StatusOut is get_status's result.
type StatusOut struct {
	Connected bool   `json:"connected"`
	Workspace string `json:"workspace,omitempty"`
	LastError string `json:"last_error,omitempty"`
	Running   []Run  `json:"running"`
	// PostsArrivedFrom lists channel IDs whose posts reached SockDock since it connected.
	PostsArrivedFrom []string `json:"posts_arrived_from" jsonschema:"channel IDs whose posts have reached SockDock since it connected"`
	// ClaudeSignedIn is whether the Claude Code that agent rules run is signed in.
	ClaudeSignedIn *bool `json:"claude_signed_in,omitempty" jsonschema:"whether the Claude Code that agent rules run is signed in; left out when unknown or no rule runs Claude Code"`
}

func (t *tools) getStatus(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, StatusOut, error) {
	st, err := t.b.Status(ctx)
	if err != nil {
		return nil, StatusOut{}, errText(err)
	}
	out := StatusOut{Connected: st.Connected, Workspace: st.Identity.Team, LastError: st.LastError, Running: []Run{}, PostsArrivedFrom: st.Heard}
	if out.PostsArrivedFrom == nil {
		out.PostsArrivedFrom = []string{}
	}
	for _, r := range st.Running {
		out.Running = append(out.Running, Run{ID: r.TurnID, Rule: r.Route, Status: store.StatusRunning, StartedAt: r.StartedAt, ThreadURL: r.Permalink})
	}
	if v, err := t.b.CheckClaude(ctx, false); err == nil {
		out.ClaudeSignedIn = v
	}
	return nil, out, nil
}

// RulesOut is list_rules's result.
type RulesOut struct {
	Rules []Rule `json:"rules"`
}

func (t *tools) listRules(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, RulesOut, error) {
	list, err := t.b.Rules(ctx)
	if err != nil {
		return nil, RulesOut{}, errText(err)
	}
	heard := t.heard(ctx)
	out := RulesOut{Rules: []Rule{}}
	// Group by channel, keeping the order within each channel.
	var channels []string
	byChannel := map[string][]Rule{}
	for _, ri := range list {
		ch := ri.Form.Channel
		if _, ok := byChannel[ch]; !ok {
			channels = append(channels, ch)
		}
		r := ruleOf(ri)
		r.PostsArrived = heard(ch)
		byChannel[ch] = append(byChannel[ch], r)
	}
	for _, ch := range channels {
		out.Rules = append(out.Rules, byChannel[ch]...)
	}
	return nil, out, nil
}

// heard returns a lookup of whether a channel's posts reached SockDock; it
// answers nil (unknown) when SockDock is not connected.
func (t *tools) heard(ctx context.Context) func(channel string) *bool {
	st, err := t.b.Status(ctx)
	if err != nil || !st.Connected {
		return func(string) *bool { return nil }
	}
	set := map[string]bool{}
	for _, ch := range st.Heard {
		set[ch] = true
	}
	return func(ch string) *bool { v := set[ch]; return &v }
}

// NameIn names a rule.
type NameIn struct {
	Name string `json:"name" jsonschema:"the rule's name (from sockdock_list_rules)"`
}

func (t *tools) getRule(ctx context.Context, _ *mcp.CallToolRequest, in NameIn) (*mcp.CallToolResult, RuleDetail, error) {
	ri, err := t.b.Rule(ctx, in.Name)
	if err != nil {
		return nil, RuleDetail{}, errText(err)
	}
	f := ri.Form
	r := ruleOf(ri)
	r.PostsArrived = t.heard(ctx)(f.Channel)
	return nil, RuleDetail{
		Rule: r, Instructions: f.Instructions, AllowWeb: f.AllowWeb, AllowEdit: f.AllowEdit,
		AllowedCommands: f.Commands, OtherTools: f.OtherTools, Model: f.Model, PromptFile: f.PromptFile, TimeoutSec: f.TimeoutSec,
	}, nil
}

// RunsIn filters list_runs.
type RunsIn struct {
	Rule  string `json:"rule,omitempty" jsonschema:"only this rule's runs"`
	Limit int    `json:"limit,omitempty" jsonschema:"at most this many (default 20, max 100)"`
}

// RunsOut is list_runs's result.
type RunsOut struct {
	Runs []Run `json:"runs"`
}

func (t *tools) listRuns(ctx context.Context, _ *mcp.CallToolRequest, in RunsIn) (*mcp.CallToolResult, RunsOut, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	limit = min(limit, 100)
	var (
		turns []store.Turn
		err   error
	)
	if in.Rule != "" {
		turns, err = t.b.RouteTurns(ctx, in.Rule, limit)
	} else {
		turns, err = t.b.Turns(ctx, limit)
	}
	if err != nil {
		return nil, RunsOut{}, errText(err)
	}
	out := RunsOut{Runs: []Run{}}
	for _, tr := range turns {
		out.Runs = append(out.Runs, runOf(tr))
	}
	return nil, out, nil
}

// Channel is a Slack channel.
type Channel struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Private bool   `json:"private"`
}

// ChannelsOut is list_channels's result.
type ChannelsOut struct {
	Channels []Channel `json:"channels"`
	Problem  string    `json:"problem,omitempty" jsonschema:"why the list is incomplete, e.g. missing_scope (add channels:read and groups:read to the Slack app)"`
}

func (t *tools) listChannels(ctx context.Context, _ *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, ChannelsOut, error) {
	res, err := t.b.Channels(ctx)
	if err != nil {
		return nil, ChannelsOut{}, errText(err)
	}
	out := ChannelsOut{Channels: []Channel{}, Problem: res.Problem}
	for _, c := range res.Channels {
		out.Channels = append(out.Channels, Channel{ID: c.ID, Name: c.Name, Private: c.Private})
	}
	return nil, out, nil
}

// resolveChannel accepts a channel ID, a link or a name ("#ask", "ask").
func (t *tools) resolveChannel(ctx context.Context, s string) (string, error) {
	id := rules.NormalizeChannel(s)
	if rules.IsChannelID(id) || id == "" {
		return id, nil
	}
	name := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "#"), "🔒")
	res, err := t.b.Channels(ctx)
	if err != nil {
		return "", errText(err)
	}
	for _, c := range res.Channels {
		if c.Name == name {
			return c.ID, nil
		}
	}
	if res.Problem != "" {
		return "", fmt.Errorf("cannot look up channel %q (%s); pass the channel ID instead", s, res.Problem)
	}
	return "", fmt.Errorf("the Slack app is not in a channel named %q. Check sockdock_list_channels; if it is missing, ask the user to type /invite in that channel", s)
}

// MatchIn is a post to test.
type MatchIn struct {
	Channel string `json:"channel" jsonschema:"channel name (#dev-help or dev-help) or ID; must be in sockdock_list_channels"`
	Text    string `json:"text" jsonschema:"the post's text"`
	User    string `json:"user,omitempty" jsonschema:"the poster's Slack member ID (from sockdock_find_users); rules this person may not run are passed over, as for a real post. Omit to ignore who posts"`
}

// MatchOut is test_match's result.
type MatchOut struct {
	Matched bool              `json:"matched"`
	Rule    string            `json:"rule,omitempty"`
	Values  map[string]string `json:"values,omitempty" jsonschema:"what the rule would pass to a program (text, or the regular expression's named groups)"`
	// AllowedUsers is set when only some people may run the matched rule.
	AllowedUsers []string `json:"allowed_users,omitempty" jsonschema:"member IDs of the only people who may run the matched rule; a post by anyone else does nothing (or goes to a later rule)"`
}

func (t *tools) testMatch(ctx context.Context, _ *mcp.CallToolRequest, in MatchIn) (*mcp.CallToolResult, MatchOut, error) {
	ch, err := t.resolveChannel(ctx, in.Channel)
	if err != nil {
		return nil, MatchOut{}, err
	}
	res, err := t.b.Match(ctx, control.MatchRequest{Channel: ch, Text: in.Text, User: strings.TrimSpace(in.User)})
	if err != nil {
		return nil, MatchOut{}, errText(err)
	}
	return nil, MatchOut{Matched: res.Matched, Rule: res.Route, Values: res.Groups, AllowedUsers: res.AllowedUsers}, nil
}

// FindIn searches people.
type FindIn struct {
	Name string `json:"name" jsonschema:"part of a display name or real name (case-insensitive), or a member ID"`
}

// FindOut is find_users's result.
type FindOut struct {
	People  []Person `json:"people"`
	Problem string   `json:"problem,omitempty" jsonschema:"why people cannot be listed: missing_scope (the Slack app lacks users:read; ask the user for member IDs, or to add users:read and reinstall the app), unavailable (not connected) or error"`
}

func (t *tools) findUsers(ctx context.Context, _ *mcp.CallToolRequest, in FindIn) (*mcp.CallToolResult, FindOut, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, FindOut{}, errors.New("give part of the person's name")
	}
	res, err := t.b.Users(ctx, in.Name, 10)
	if err != nil {
		return nil, FindOut{}, errText(err)
	}
	out := FindOut{People: []Person{}, Problem: res.Problem}
	for _, m := range res.Members {
		out.People = append(out.People, Person{ID: m.ID, Name: m.Name, RealName: m.RealName})
	}
	return nil, out, nil
}

// SaveIn adds or changes a rule. For a change, omitted fields keep their values.
type SaveIn struct {
	Name            string    `json:"name,omitempty" jsonschema:"the rule to change; omit to add a rule"`
	Channel         *string   `json:"channel,omitempty" jsonschema:"channel name (#dev-help or dev-help) or ID; must be in sockdock_list_channels"`
	ConditionType   *string   `json:"condition_type,omitempty" jsonschema:"starts_with (default for a new rule), contains, match or all_posts"`
	Keyword         *string   `json:"keyword,omitempty" jsonschema:"the keyword, or the regular expression (RE2) for match; not used for all_posts"`
	Runs            *string   `json:"runs,omitempty" jsonschema:"agent (Claude Code, default for a new rule) or program"`
	Folder          *string   `json:"folder,omitempty" jsonschema:"working folder (absolute or ~/...); for a program, defaults to the program's folder"`
	Instructions    *string   `json:"instructions,omitempty" jsonschema:"agent: instructions added to its system prompt"`
	AllowWeb        *bool     `json:"allow_web,omitempty" jsonschema:"agent: may search and fetch the web"`
	AllowEdit       *bool     `json:"allow_edit,omitempty" jsonschema:"agent: may edit files in the folder"`
	AllowedCommands *[]string `json:"allowed_commands,omitempty" jsonschema:"agent: command prefixes it may run, e.g. 'npm test'"`
	AllowedUsers    *[]string `json:"allowed_users,omitempty" jsonschema:"member IDs (U…, from sockdock_find_users) of the only people who may run the rule; [] lets everyone in the channel run it; omitted on a new rule means everyone"`
	Model           *string   `json:"model,omitempty" jsonschema:"agent: opus, sonnet, haiku or empty for the default"`
	Program         *string   `json:"program,omitempty" jsonschema:"program: path to the executable; gets the post as JSON on stdin, its stdout is the reply"`
	Args            *string   `json:"args,omitempty" jsonschema:"program: arguments"`
	TimeoutSec      *int      `json:"timeout_sec,omitempty" jsonschema:"time limit in seconds; 0 for the default (600 for agents, 300 for programs)"`
}

// SaveOut is the result of a change.
type SaveOut struct {
	OK             bool      `json:"ok"`
	Name           string    `json:"name,omitempty"`
	Problems       []Problem `json:"problems,omitempty" jsonschema:"what to fix, per field (nothing was saved)"`
	ConfigProblems []string  `json:"config_problems,omitempty"`
	Warning        string    `json:"warning,omitempty" jsonschema:"saved, but tell the user this"`
}

func saveOut(r control.RuleSaveResult) SaveOut {
	out := SaveOut{OK: r.OK, Name: r.Name, Problems: problemsOf(r.Problems), ConfigProblems: r.ConfigProblems}
	switch r.OpenToAll {
	case "commands":
		out.Warning = "Everyone in this channel can make this Mac run the allowed commands. If that is not what the user wants, set allowed_users to the people who may run the rule."
	case "edit":
		out.Warning = "Everyone in this channel can make the agent change files in the working folder. If that is not what the user wants, set allowed_users to the people who may run the rule."
	}
	return out
}

func (t *tools) saveRule(ctx context.Context, _ *mcp.CallToolRequest, in SaveIn) (*mcp.CallToolResult, SaveOut, error) {
	f := rules.Form{Trigger: config.KindStartsWith, Type: config.TypeClaude, Commands: []string{}, OtherTools: []string{}}
	if in.Name != "" {
		ri, err := t.b.Rule(ctx, in.Name)
		if err != nil {
			return nil, SaveOut{}, errText(err)
		}
		f = ri.Form
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	if in.Channel != nil {
		ch, err := t.resolveChannel(ctx, *in.Channel)
		if err != nil {
			return nil, SaveOut{}, err
		}
		f.Channel = ch
	}
	set(&f.Trigger, in.ConditionType)
	set(&f.Keyword, in.Keyword)
	if in.Runs != nil {
		switch *in.Runs {
		case "agent":
			f.Type = config.TypeClaude
		case "program":
			f.Type = config.TypeCommand
		default:
			return nil, SaveOut{}, fmt.Errorf("runs must be agent or program, not %q", *in.Runs)
		}
	}
	set(&f.Folder, in.Folder)
	set(&f.Instructions, in.Instructions)
	if in.AllowWeb != nil {
		f.AllowWeb = *in.AllowWeb
	}
	if in.AllowEdit != nil {
		f.AllowEdit = *in.AllowEdit
	}
	if in.AllowedCommands != nil {
		f.Commands = *in.AllowedCommands
	}
	if in.AllowedUsers != nil {
		f.AllowedUsers = *in.AllowedUsers
		f.Restricted = len(f.AllowedUsers) > 0
	}
	set(&f.Model, in.Model)
	set(&f.Program, in.Program)
	set(&f.Args, in.Args)
	if in.TimeoutSec != nil {
		f.TimeoutSec = *in.TimeoutSec
	}
	if f.Type == config.TypeCommand && f.Folder == "" && strings.Contains(f.Program, "/") {
		f.Folder = filepath.Dir(f.Program)
	}
	res, err := t.b.SaveRule(ctx, f)
	if err != nil {
		return nil, SaveOut{}, errText(err)
	}
	return nil, saveOut(res), nil
}

// ChangeOut is the result of a delete or reorder.
type ChangeOut struct {
	OK             bool     `json:"ok"`
	ConfigProblems []string `json:"config_problems,omitempty" jsonschema:"problems in the whole config after the change (rare)"`
}

func (t *tools) deleteRule(ctx context.Context, _ *mcp.CallToolRequest, in NameIn) (*mcp.CallToolResult, ChangeOut, error) {
	res, err := t.b.DeleteRule(ctx, in.Name)
	if err != nil {
		return nil, ChangeOut{}, errText(err)
	}
	return nil, ChangeOut{OK: res.OK, ConfigProblems: res.ConfigProblems}, nil
}

// OrderIn orders a channel's rules.
type OrderIn struct {
	Channel string   `json:"channel" jsonschema:"channel name (#dev-help or dev-help) or ID; must be in sockdock_list_channels"`
	Names   []string `json:"names" jsonschema:"every rule of the channel once, in the new order"`
}

func (t *tools) reorderRules(ctx context.Context, _ *mcp.CallToolRequest, in OrderIn) (*mcp.CallToolResult, ChangeOut, error) {
	ch, err := t.resolveChannel(ctx, in.Channel)
	if err != nil {
		return nil, ChangeOut{}, err
	}
	res, err := t.b.OrderRules(ctx, control.OrderRequest{Channel: ch, Names: in.Names})
	if err != nil {
		return nil, ChangeOut{}, errText(err)
	}
	return nil, ChangeOut{OK: res.OK, ConfigProblems: res.ConfigProblems}, nil
}

// EnabledIn turns a rule on or off.
type EnabledIn struct {
	Name    string `json:"name" jsonschema:"the rule's name (from sockdock_list_rules)"`
	Enabled bool   `json:"enabled" jsonschema:"true to turn the rule on, false to turn it off"`
}

// OKOut is a plain success.
type OKOut struct {
	OK bool `json:"ok"`
}

func (t *tools) setRuleEnabled(ctx context.Context, _ *mcp.CallToolRequest, in EnabledIn) (*mcp.CallToolResult, OKOut, error) {
	if err := t.b.SetPaused(ctx, in.Name, !in.Enabled); err != nil {
		return nil, OKOut{}, errText(err)
	}
	return nil, OKOut{OK: true}, nil
}

// StopIn names a run.
type StopIn struct {
	ID int64 `json:"id" jsonschema:"the run's id"`
}

func (t *tools) stopRun(ctx context.Context, _ *mcp.CallToolRequest, in StopIn) (*mcp.CallToolResult, OKOut, error) {
	if err := t.b.StopTurn(ctx, in.ID); err != nil {
		return nil, OKOut{}, errText(err)
	}
	return nil, OKOut{OK: true}, nil
}

// ToolInfo is a tool's name and description (for an MCPB manifest).
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Tools lists the tools the server offers, by asking a server instance
// over an in-memory connection (so the list never drifts from New).
func Tools(ctx context.Context) ([]ToolInfo, error) {
	ct, st := mcp.NewInMemoryTransports()
	if _, err := New(nil, "").Connect(ctx, st, nil); err != nil {
		return nil, err
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "sockdock-tools"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		return nil, err
	}
	defer cs.Close()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := make([]ToolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		out = append(out, ToolInfo{Name: t.Name, Description: t.Description})
	}
	return out, nil
}
