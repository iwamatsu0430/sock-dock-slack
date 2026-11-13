package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

type fake struct {
	heard  []string
	turns  []store.Turn
	down   bool
	rules  []control.RuleInfo
	saved  *rules.Form
	order  *control.OrderRequest
	paused map[string]bool
}

func (f *fake) err() error {
	if f.down {
		return control.ErrNotRunning
	}
	return nil
}
func (f *fake) Status(context.Context) (gateway.Status, error) {
	return gateway.Status{Connected: true, Identity: slackio.Identity{Team: "acme"}, Heard: f.heard}, f.err()
}
func (f *fake) Rules(context.Context) ([]control.RuleInfo, error) { return f.rules, f.err() }
func (f *fake) Rule(_ context.Context, name string) (control.RuleInfo, error) {
	for _, r := range f.rules {
		if r.Form.Name == name {
			return r, nil
		}
	}
	return control.RuleInfo{}, &control.APIError{Status: 404, Message: "no rule named " + name}
}
func (f *fake) Turns(context.Context, int) ([]store.Turn, error) {
	return []store.Turn{{ID: 1, Route: "a", Status: "error", Error: strings.Repeat("x", 900)}}, f.err()
}
func (f *fake) RouteTurns(context.Context, string, int) ([]store.Turn, error) {
	return f.turns, f.err()
}
func (f *fake) Channels(context.Context) (control.ChannelsResult, error) {
	return control.ChannelsResult{Channels: []slackio.Channel{{ID: "C0123ASK", Name: "ask"}}}, f.err()
}
func (f *fake) Match(_ context.Context, req control.MatchRequest) (control.MatchResult, error) {
	return control.MatchResult{Matched: req.Channel == "C0123ASK", Route: "a"}, f.err()
}
func (f *fake) SaveRule(_ context.Context, fm rules.Form) (control.RuleSaveResult, error) {
	f.saved = &fm
	return control.RuleSaveResult{OK: true, Name: "rule-9"}, f.err()
}
func (f *fake) DeleteRule(context.Context, string) (control.RuleSaveResult, error) {
	return control.RuleSaveResult{OK: true}, f.err()
}
func (f *fake) OrderRules(_ context.Context, req control.OrderRequest) (control.RuleSaveResult, error) {
	f.order = &req
	return control.RuleSaveResult{OK: true}, f.err()
}
func (f *fake) SetPaused(_ context.Context, name string, p bool) error {
	f.paused[name] = p
	return f.err()
}
func (f *fake) StopTurn(context.Context, int64) error { return f.err() }

func connect(t *testing.T, b Backend) *mcp.ClientSession {
	t.Helper()
	ct, st := mcp.NewInMemoryTransports()
	ctx := context.Background()
	if _, err := New(b, "test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out != nil && !res.IsError {
		b, _ := json.Marshal(res.StructuredContent)
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return res
}

func sample() *fake {
	form := func(name, ch, trig, kw string) rules.Form {
		return rules.Form{Name: name, OriginalName: name, Channel: ch, Trigger: trig, Keyword: kw, Type: "claude", Folder: "~/app",
			Instructions: "be brief", AllowWeb: true, Commands: []string{"make test"}, OtherTools: []string{}}
	}
	return &fake{paused: map[string]bool{}, rules: []control.RuleInfo{
		{Row: rules.Row{Form: form("a", "C0123ASK", "all_posts", "")}, ChannelName: "#ask"},
		{Row: rules.Row{Form: form("b", "C0123OPS", "starts_with", "deploy")}},
		{Row: rules.Row{Form: form("c", "C0123ASK", "starts_with", "review"), ShadowedBy: &rules.Condition{Trigger: "all_posts"}}, ChannelName: "#ask"},
	}}
}

func TestToolsAndAnnotations(t *testing.T) {
	cs := connect(t, sample())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reads := map[string]bool{}
	for _, tl := range res.Tools {
		reads[tl.Name] = tl.Annotations != nil && tl.Annotations.ReadOnlyHint
	}
	want := map[string]bool{"sockdock_get_status": true, "sockdock_list_rules": true, "sockdock_get_rule": true, "sockdock_list_runs": true, "sockdock_list_channels": true, "sockdock_test_match": true,
		"sockdock_check_rule": true, "sockdock_save_rule": false, "sockdock_delete_rule": false, "sockdock_reorder_rules": false, "sockdock_set_rule_enabled": false, "sockdock_stop_run": false}
	if len(reads) != len(want) {
		t.Errorf("tools = %v", reads)
	}
	for name, ro := range want {
		if r, ok := reads[name]; !ok || r != ro {
			t.Errorf("%s: read-only = %v (present %v), want %v", name, r, ok, ro)
		}
	}
}

func TestListRulesGroupsByChannel(t *testing.T) {
	cs := connect(t, sample())
	var out RulesOut
	call(t, cs, "sockdock_list_rules", nil, &out)
	var names []string
	for _, r := range out.Rules {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "a,c,b" || out.Rules[1].ShadowedBy == nil || out.Rules[0].ChannelName != "#ask" {
		t.Errorf("rules = %+v", out.Rules)
	}
}

func TestSaveRuleKeepsOmittedFields(t *testing.T) {
	f := sample()
	cs := connect(t, f)
	var out SaveOut
	call(t, cs, "sockdock_save_rule", map[string]any{"name": "b", "keyword": "ship", "channel": "#ask"}, &out)
	s := f.saved
	if !out.OK || s == nil || s.OriginalName != "b" || s.Keyword != "ship" || s.Channel != "C0123ASK" ||
		s.Instructions != "be brief" || !s.AllowWeb || len(s.Commands) != 1 {
		t.Fatalf("saved = %+v, out = %+v", s, out)
	}

	call(t, cs, "sockdock_save_rule", map[string]any{"channel": "C0123ASK", "runs": "program", "program": "/usr/local/bin/x", "condition_type": "contains", "keyword": "help"}, &out)
	s = f.saved
	if s.OriginalName != "" || s.Type != "command" || s.Folder != "/usr/local/bin" || s.Trigger != "contains" {
		t.Errorf("new program rule = %+v", s)
	}

	res := call(t, cs, "sockdock_save_rule", map[string]any{"channel": "#nowhere"}, nil)
	if !res.IsError {
		t.Error("an unknown channel name should be an error")
	}
}

func TestOtherTools(t *testing.T) {
	f := sample()
	cs := connect(t, f)
	var runs RunsOut
	call(t, cs, "sockdock_list_runs", nil, &runs)
	if len(runs.Runs) != 1 || len(runs.Runs[0].Error) > 510 {
		t.Errorf("runs = %+v", runs)
	}
	var m MatchOut
	call(t, cs, "sockdock_test_match", map[string]any{"channel": "ask", "text": "hi"}, &m)
	if !m.Matched {
		t.Errorf("match = %+v", m)
	}
	call(t, cs, "sockdock_reorder_rules", map[string]any{"channel": "#ask", "names": []string{"c", "a"}}, nil)
	if f.order == nil || f.order.Channel != "C0123ASK" {
		t.Errorf("order = %+v", f.order)
	}
	call(t, cs, "sockdock_set_rule_enabled", map[string]any{"name": "a", "enabled": false}, nil)
	if !f.paused["a"] {
		t.Error("rule a should be off")
	}
}

func TestNotRunning(t *testing.T) {
	f := sample()
	f.down = true
	cs := connect(t, f)
	res := call(t, cs, "sockdock_get_status", nil, nil)
	if !res.IsError {
		t.Fatal("want an error")
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "not running") {
		t.Errorf("error = %q", text)
	}
}

func TestCheckRule(t *testing.T) {
	f := sample()
	f.heard = []string{"C0123ASK"}
	cs := connect(t, f)
	var out CheckOut
	call(t, cs, "sockdock_check_rule", map[string]any{"name": "a"}, &out)
	if !out.CanRun {
		t.Errorf("rule a should be able to run: %+v", out.Findings)
	}
	f.turns = []store.Turn{{ID: 9, Route: "b", Status: "error", Error: "exit 1"}}
	call(t, cs, "sockdock_check_rule", map[string]any{"name": "b"}, &out)
	var failed []string
	for _, x := range out.Findings {
		if !x.OK {
			failed = append(failed, x.Check)
		}
	}
	if out.CanRun || strings.Join(failed, ",") != "posts arrive,recent runs" {
		t.Errorf("rule b findings = %+v", out.Findings)
	}
	f.turns = nil
	call(t, cs, "sockdock_check_rule", map[string]any{"name": "c"}, &out)
	if out.CanRun || !strings.Contains(fmt.Sprint(out.Findings), "sockdock_reorder_rules") {
		t.Errorf("shadowed rule c should point to reordering: %+v", out.Findings)
	}
}

func TestPostsArrivedInList(t *testing.T) {
	f := sample()
	f.heard = []string{"C0123ASK"}
	cs := connect(t, f)
	var out RulesOut
	call(t, cs, "sockdock_list_rules", nil, &out)
	got := map[string]bool{}
	for _, r := range out.Rules {
		if r.PostsArrived == nil {
			t.Fatalf("%s: posts_arrived unknown", r.Name)
		}
		got[r.Name] = *r.PostsArrived
	}
	if !got["a"] || got["b"] {
		t.Errorf("posts_arrived = %v", got)
	}
}

func TestProblemMessages(t *testing.T) {
	m := problemMessage(rules.FieldProblem{Field: "folder", Code: "not_dir", Detail: "/nope"})
	if !strings.Contains(m, "/nope") || !strings.Contains(m, "does not exist") {
		t.Errorf("folder message = %q", m)
	}
	if m := problemMessage(rules.FieldProblem{Field: "x", Code: "odd_thing"}); m != "x: odd thing" {
		t.Errorf("fallback = %q", m)
	}
}

func TestDescriptionsAreDetailed(t *testing.T) {
	cs := connect(t, sample())
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tl := range res.Tools {
		if !strings.HasPrefix(tl.Name, "sockdock_") {
			t.Errorf("%s: missing sockdock_ prefix", tl.Name)
		}
		if n := strings.Count(tl.Description, ". "); n < 2 {
			t.Errorf("%s: description has %d sentences, want 3 or more", tl.Name, n+1)
		}
	}
}
