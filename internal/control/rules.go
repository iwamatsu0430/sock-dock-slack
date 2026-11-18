package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/rules"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// SourceHeader names who made a change ("mcp" for `sockdock mcp`). Changes
// with a source are announced as rule_changed events so the app can notify.
const SourceHeader = "X-SockDock-Source"

// RuleInfo is a rule with its state, as the control API lists it.
type RuleInfo struct {
	rules.Row
	// ChannelName is "#name" (or "🔒name") when Slack tells us, else "".
	ChannelName string      `json:"channel_name,omitempty"`
	Last        *store.Turn `json:"last,omitempty"`
	// Users are the people in Form.AllowedUsers, in the same order. Name is
	// "" for an ID Slack does not know or when users:read is missing.
	Users []slackio.Member `json:"users,omitempty"`
}

// RuleSaveResult is the outcome of saving a rule.
type RuleSaveResult struct {
	OK bool `json:"ok"`
	// Name is the saved rule's name (a new rule gets one).
	Name     string               `json:"name,omitempty"`
	Problems []rules.FieldProblem `json:"problems,omitempty"`
	// ConfigProblems are problems in the whole config after the change.
	ConfigProblems []string `json:"config_problems,omitempty"`
	// OpenToAll is "commands" or "edit" when everyone in the channel can now
	// make the agent run commands or edit files (rules.OpenToAll).
	OpenToAll string `json:"open_to_all,omitempty"`
}

// UsersResult lists people in the workspace, or why it cannot.
type UsersResult struct {
	Members []slackio.Member `json:"members"`
	// Problem is "missing_scope", "unavailable" or "error" (see Error).
	Problem string `json:"problem,omitempty"`
	Error   string `json:"error,omitempty"`
}

// OrderRequest orders one channel's rules.
type OrderRequest struct {
	Channel string   `json:"channel"`
	Names   []string `json:"names"`
}

// ChannelsResult lists the channels the Slack app is in, or why it cannot.
type ChannelsResult struct {
	Channels []slackio.Channel `json:"channels"`
	// Problem is "missing_scope", "unavailable" or "error" (see Error).
	Problem string `json:"problem,omitempty"`
	Error   string `json:"error,omitempty"`
}

// RuleChange is the data of a rule_changed event.
type RuleChange struct {
	Source string `json:"source"`
	// Action is "added", "changed", "deleted", "reordered", "enabled" or "disabled".
	Action  string `json:"action"`
	Rule    string `json:"rule,omitempty"`
	Channel string `json:"channel"`
	Trigger string `json:"trigger,omitempty"`
	Keyword string `json:"keyword,omitempty"`
}

// ruleState holds what the rule endpoints share.
type ruleState struct {
	// mu serializes read-modify-write of the config by the rule endpoints.
	mu sync.Mutex

	chMu     sync.Mutex
	chNames  map[string]string
	chExpiry time.Time

	membersOnce sync.Once
	members     *slackio.MemberDirectory
}

// memberDir returns the kept list of the workspace's people.
func (s *Server) memberDir() *slackio.MemberDirectory {
	s.rules.membersOnce.Do(func() {
		s.rules.members = &slackio.MemberDirectory{Fetch: func(ctx context.Context) ([]slackio.Member, error) {
			l, ok := s.gw.API().(slackio.MemberLister)
			if !ok {
				return nil, errUnavailable
			}
			return l.Members(ctx)
		}}
	})
	return s.rules.members
}

var errUnavailable = errors.New("not connected to Slack")

// usersOf names the member IDs of a rule, keeping their order.
func usersOf(ids []string, names map[string]slackio.Member) []slackio.Member {
	if len(ids) == 0 {
		return nil
	}
	out := make([]slackio.Member, 0, len(ids))
	for _, id := range ids {
		m, ok := names[id]
		if !ok {
			m = slackio.Member{ID: id}
		}
		out = append(out, m)
	}
	return out
}

func (s *Server) configPath() string { return s.gw.Config().Path }

// writeConfig validates, writes and reloads the config.
func (s *Server) writeConfig(content []byte) (ValidateResult, error) {
	path := s.configPath()
	if res := validate(path, content); !res.OK {
		return res, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return ValidateResult{}, err
	}
	_, werr := tmp.Write(content)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o600), os.Rename(tmp.Name(), path)); err != nil {
		os.Remove(tmp.Name())
		return ValidateResult{}, err
	}
	if err := s.gw.Reload(); err != nil {
		return ValidateResult{}, err
	}
	return ValidateResult{OK: true}, nil
}

func (s *Server) announce(r *http.Request, c RuleChange) {
	if c.Source = r.Header.Get(SourceHeader); c.Source == "" {
		return
	}
	c.Channel = rules.NormalizeChannel(c.Channel)
	s.log.Info("rule changed", "source", c.Source, "action", c.Action, "rule", c.Rule)
	s.gw.Announce(gateway.Event{Type: "rule_changed", Data: c})
	if c.Source == "mcp" {
		s.gw.NotifyRuleChanged(c.Channel, c.Trigger, c.Keyword)
	}
}

func changeOf(f rules.Form) RuleChange {
	return RuleChange{Rule: f.Name, Channel: f.Channel, Trigger: f.Trigger, Keyword: f.Keyword}
}

func (s *Server) ruleInfos(ctx context.Context) ([]RuleInfo, error) {
	_, raw, err := rules.Read(s.configPath())
	if err != nil {
		return nil, err
	}
	off := map[string]bool{}
	for _, r := range s.gw.Status(os.Getpid()).Routes {
		off[r.Name] = r.Paused
	}
	last := map[string]*store.Turn{}
	if turns, err := s.gw.Turns(ctx, 200); err == nil {
		for _, t := range turns {
			if _, ok := last[t.Route]; !ok {
				last[t.Route] = &t
			}
		}
	}
	names := s.channelNames(ctx)
	var ids []string
	for _, r := range raw.Routes {
		ids = append(ids, r.AllowedUsers...)
	}
	people := s.memberDir().Names(ctx, ids)
	out := []RuleInfo{}
	for _, row := range rules.List(raw, func(n string) bool { return off[n] }) {
		out = append(out, RuleInfo{Row: row, ChannelName: names[row.Form.Channel], Last: last[row.Form.Name], Users: usersOf(row.Form.AllowedUsers, people)})
	}
	return out, nil
}

// channelNames maps channel IDs to "#name", refreshed at most once a minute.
func (s *Server) channelNames(ctx context.Context) map[string]string {
	s.rules.chMu.Lock()
	defer s.rules.chMu.Unlock()
	if s.rules.chNames != nil && time.Now().Before(s.rules.chExpiry) {
		return s.rules.chNames
	}
	names := map[string]string{config.ChannelDM: "DM"}
	if l, ok := s.gw.API().(slackio.ChannelLister); ok {
		if chs, err := l.Channels(ctx); err == nil {
			for _, c := range chs {
				names[c.ID] = channelLabel(c)
			}
		}
	}
	s.rules.chNames, s.rules.chExpiry = names, time.Now().Add(time.Minute)
	return names
}

func channelLabel(c slackio.Channel) string {
	if c.Private {
		return "🔒" + c.Name
	}
	return "#" + c.Name
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	out, err := s.ruleInfos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	out, err := s.ruleInfos(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, ri := range out {
		if ri.Form.Name == r.PathValue("name") {
			writeJSON(w, http.StatusOK, ri)
			return
		}
	}
	writeError(w, http.StatusNotFound, errors.New("no rule named "+r.PathValue("name")))
}

func (s *Server) saveRule(w http.ResponseWriter, r *http.Request) {
	var f rules.Form
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&f); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.rules.mu.Lock()
	defer s.rules.mu.Unlock()
	data, raw, err := rules.Read(s.configPath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	saved, err := rules.Save(data, raw, f)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if len(saved.Problems) > 0 {
		writeJSON(w, http.StatusOK, RuleSaveResult{Problems: saved.Problems})
		return
	}
	res, err := s.writeConfig(saved.Content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !res.OK {
		writeJSON(w, http.StatusOK, RuleSaveResult{ConfigProblems: res.Problems})
		return
	}
	c := changeOf(f)
	c.Rule, c.Action = saved.Name, "changed"
	if f.OriginalName == "" {
		c.Action = "added"
	}
	s.announce(r, c)
	writeJSON(w, http.StatusOK, RuleSaveResult{OK: true, Name: saved.Name, OpenToAll: rules.OpenToAll(f)})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	s.rules.mu.Lock()
	defer s.rules.mu.Unlock()
	data, raw, err := rules.Read(s.configPath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var before rules.Form
	if rt := raw.Route(name); rt != nil {
		before = rules.ToForm(rt)
	}
	content, err := rules.Delete(data, raw, name)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	res, err := s.writeConfig(content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !res.OK {
		writeJSON(w, http.StatusOK, RuleSaveResult{ConfigProblems: res.Problems})
		return
	}
	c := changeOf(before)
	c.Action = "deleted"
	s.announce(r, c)
	writeJSON(w, http.StatusOK, RuleSaveResult{OK: true, Name: name})
}

func (s *Server) orderRules(w http.ResponseWriter, r *http.Request) {
	var req OrderRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.rules.mu.Lock()
	defer s.rules.mu.Unlock()
	data, raw, err := rules.Read(s.configPath())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	content, err := rules.ReorderChannel(data, raw, req.Channel, req.Names)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	res, err := s.writeConfig(content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !res.OK {
		writeJSON(w, http.StatusOK, RuleSaveResult{ConfigProblems: res.Problems})
		return
	}
	s.announce(r, RuleChange{Action: "reordered", Channel: req.Channel})
	writeJSON(w, http.StatusOK, RuleSaveResult{OK: true})
}

func (s *Server) channels(w http.ResponseWriter, r *http.Request) {
	l, ok := s.gw.API().(slackio.ChannelLister)
	if !ok {
		writeJSON(w, http.StatusOK, ChannelsResult{Channels: []slackio.Channel{}, Problem: "unavailable"})
		return
	}
	chs, err := l.Channels(r.Context())
	// DMs to the Slack app come first: the first choice is DM.
	dm := slackio.Channel{ID: config.ChannelDM, Name: "DM"}
	if !s.gw.Status(os.Getpid()).HasScope(slackio.ScopeIMHistory) {
		dm.Problem = "missing_scope"
	}
	chs = append([]slackio.Channel{dm}, chs...)
	if err != nil {
		problem := "error"
		if errors.Is(err, context.Canceled) {
			return
		}
		if strings.Contains(err.Error(), "missing_scope") {
			problem = "missing_scope"
		}
		writeJSON(w, http.StatusOK, ChannelsResult{Channels: chs, Problem: problem, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, ChannelsResult{Channels: chs})
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	found, err := s.memberDir().Search(r.Context(), r.URL.Query().Get("q"), limit)
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, errUnavailable):
		writeJSON(w, http.StatusOK, UsersResult{Members: []slackio.Member{}, Problem: "unavailable"})
	case slackio.IsMissingScope(err):
		writeJSON(w, http.StatusOK, UsersResult{Members: []slackio.Member{}, Problem: "missing_scope", Error: err.Error()})
	case err != nil:
		writeJSON(w, http.StatusOK, UsersResult{Members: []slackio.Member{}, Problem: "error", Error: err.Error()})
	default:
		writeJSON(w, http.StatusOK, UsersResult{Members: found})
	}
}

// pauseChange describes a pause/resume for the rule_changed event.
func (s *Server) pauseChange(name string, paused bool) RuleChange {
	c := RuleChange{Rule: name, Action: "enabled"}
	if paused {
		c.Action = "disabled"
	}
	if rt := s.gw.Config().Route(name); rt != nil {
		c.Channel, c.Trigger, c.Keyword = rt.Channel, rt.Kind(), rules.DisplayPattern(rt)
	}
	return c
}
