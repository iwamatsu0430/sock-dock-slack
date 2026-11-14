package rules

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

// ErrChanged is returned when a reorder names a different set of rules than
// the config holds (the rules changed since they were listed).
var ErrChanged = errors.New("the rules changed; list them again")

// Read reads the config file without expanding or validating it. A missing
// file is created with a minimal config first.
func Read(path string) ([]byte, *config.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(path, []byte(config.MinimalConfig), 0o600); err != nil {
			return nil, nil, err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	raw, err := config.ParseRaw(data)
	if err != nil {
		return data, nil, err
	}
	return data, raw, nil
}

// Saved is the outcome of Save.
type Saved struct {
	// Content is the new config file, unless Problems is set.
	Content []byte
	// Name is the saved rule's name (a new rule gets one).
	Name     string
	Problems []FieldProblem
	// ClaudeCommand is set when settings.claude_command was filled in.
	ClaudeCommand string
	// CodexCommand is set when settings.codex_command was filled in.
	CodexCommand string
}

// Save adds or updates a rule. A new rule, or one moved to another channel,
// goes to the bottom of its channel (rules are tried top to bottom).
func Save(data []byte, raw *config.Config, f Form) (Saved, error) {
	if strings.TrimSpace(f.Name) == "" {
		if f.OriginalName != "" {
			f.Name = f.OriginalName
		} else {
			f.Name = AutoName(raw)
		}
	}
	if f.OriginalName != "" && raw.Route(f.OriginalName) == nil {
		return Saved{}, fmt.Errorf("no rule named %q", f.OriginalName)
	}
	if ps := Validate(f, raw); len(ps) > 0 {
		return Saved{Problems: ps}, nil
	}

	var routes []*config.Route
	var moved *config.Route
	replaced := false
	for _, r := range raw.Routes {
		if f.OriginalName != "" && r.Name == f.OriginalName {
			nr := FromForm(f, r)
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
		routes = append(routes, FromForm(f, nil))
	}
	content, err := config.ReplaceRoutes(data, routes)
	if err != nil {
		return Saved{}, err
	}

	out := Saved{Name: strings.TrimSpace(f.Name)}
	// Claude Code: if the configured command cannot be found (or is a shim),
	// fill in an absolute path found on this Mac.
	if f.Type == config.TypeClaude {
		path, changed := claudeCommandFix(raw)
		switch {
		case path == "":
			return Saved{Problems: []FieldProblem{{Field: "type", Code: "claude_not_found"}}}, nil
		case changed:
			if content, err = config.SetSetting(content, "claude_command", path); err != nil {
				return Saved{}, err
			}
			out.ClaudeCommand = path
		}
	}
	// Codex: the same, for settings.codex_command.
	if f.Type == config.TypeCodex {
		path, changed := codexCommandFix(raw)
		switch {
		case path == "":
			return Saved{Problems: []FieldProblem{{Field: "type", Code: "codex_not_found"}}}, nil
		case changed:
			if content, err = config.SetSetting(content, "codex_command", path); err != nil {
				return Saved{}, err
			}
			out.CodexCommand = path
		}
	}
	out.Content = content
	return out, nil
}

// Delete removes a rule.
func Delete(data []byte, raw *config.Config, name string) ([]byte, error) {
	if raw.Route(name) == nil {
		return nil, fmt.Errorf("no rule named %q", name)
	}
	routes := slices.DeleteFunc(slices.Clone(raw.Routes), func(r *config.Route) bool { return r.Name == name })
	return config.ReplaceRoutes(data, routes)
}

// Reorder writes the rules in the given order (every rule name once).
func Reorder(data []byte, raw *config.Config, names []string) ([]byte, error) {
	if len(names) != len(raw.Routes) {
		return nil, ErrChanged
	}
	byName := map[string]*config.Route{}
	for _, r := range raw.Routes {
		byName[r.Name] = r
	}
	routes := make([]*config.Route, 0, len(names))
	for _, n := range names {
		r, ok := byName[n]
		if !ok {
			return nil, ErrChanged
		}
		delete(byName, n)
		routes = append(routes, r)
	}
	return config.ReplaceRoutes(data, routes)
}

// ReorderChannel orders one channel's rules (every rule of that channel
// once); the other channels' rules keep their places.
func ReorderChannel(data []byte, raw *config.Config, channel string, names []string) ([]byte, error) {
	channel = NormalizeChannel(channel)
	var slots []int
	for i, r := range raw.Routes {
		if r.Channel == channel {
			slots = append(slots, i)
		}
	}
	if len(slots) != len(names) {
		return nil, ErrChanged
	}
	routes := slices.Clone(raw.Routes)
	used := map[string]bool{}
	for k, n := range names {
		r := raw.Route(n)
		if r == nil || r.Channel != channel || used[n] {
			return nil, ErrChanged
		}
		used[n] = true
		routes[slots[k]] = r
	}
	return config.ReplaceRoutes(data, routes)
}

// Condition is a rule's condition as lists show it.
type Condition struct {
	Trigger string `json:"trigger"`
	Keyword string `json:"keyword"`
}

// Row is a rule with what keeps it from running.
type Row struct {
	Form Form `json:"form"`
	// Off is true when the rule is turned off.
	Off bool `json:"off"`
	// Problem is why the rule cannot run, if anything.
	Problem *FieldProblem `json:"problem,omitempty"`
	// ShadowedBy is the condition of an earlier rule in the same channel that
	// every matching post meets first, so this rule never runs.
	ShadowedBy *Condition `json:"shadowed_by,omitempty"`
}

// List returns the rules in config order with their problems.
func List(raw *config.Config, off func(name string) bool) []Row {
	out := make([]Row, 0, len(raw.Routes))
	for i, r := range raw.Routes {
		f := ToForm(r)
		row := Row{Form: f, Off: off != nil && off(r.Name)}
		for _, p := range Validate(f, raw) {
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
	return out
}

// claudeCommandFix returns a usable Claude Code path. changed is true when the
// config should be updated to it; path is "" when none was found.
func claudeCommandFix(raw *config.Config) (path string, changed bool) {
	cur := []string(raw.Settings.ClaudeCommand)
	if len(cur) > 0 && cur[0] != "" && cur[0] != "claude" {
		p := paths.ExpandHome(cur[0])
		if st, err := os.Stat(p); err == nil && !st.IsDir() && !strings.Contains(p, "/shims/") {
			return p, false
		}
	}
	found := DetectClaude()
	if len(found) == 0 {
		return "", false
	}
	return found[0], true
}

// codexCommandFix is claudeCommandFix for settings.codex_command.
func codexCommandFix(raw *config.Config) (path string, changed bool) {
	cur := []string(raw.Settings.CodexCommand)
	if len(cur) > 0 && cur[0] != "" && cur[0] != "codex" {
		p := paths.ExpandHome(cur[0])
		if st, err := os.Stat(p); err == nil && !st.IsDir() && !strings.Contains(p, "/shims/") {
			return p, false
		}
	}
	found := DetectCodex()
	if len(found) == 0 {
		return "", false
	}
	return found[0], true
}
