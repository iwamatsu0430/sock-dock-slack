// Package rules edits the config's rules the way the app's form does. The
// menu bar app and the control API (used by `sockdock mcp`) share it, so a
// rule is checked and written the same way whichever one changes it.
package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

// Form is a rule as the form edits it: plain choices instead of raw YAML.
type Form struct {
	// OriginalName is the name before editing ("" for a new rule).
	OriginalName string `json:"original_name"`
	// Name is the rule's name in the config. A new rule gets one automatically.
	Name string `json:"name"`
	// Channel is a channel ID or a pasted channel link.
	Channel string `json:"channel"`
	// Trigger is the condition kind: "starts_with", "contains", "match" (a
	// regex) or "all_posts". Keyword is the keyword or the regex.
	Trigger string `json:"trigger"`
	Keyword string `json:"keyword"`
	// Restricted is true when only AllowedUsers may run the rule ("実行できる
	// 人": 選んだ人); false means everyone in the channel (全員).
	Restricted bool `json:"restricted"`
	// AllowedUsers are Slack member IDs, in the order they were added.
	AllowedUsers []string `json:"allowed_users"`
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

var (
	webTools  = []string{"WebSearch", "WebFetch"}
	editTools = []string{"Edit", "Write"}
	bashRule  = regexp.MustCompile(`^Bash\((.+?) \*\)$`)
	channelID = regexp.MustCompile(`^[CGD][A-Z0-9]{6,}$`)
	linkID    = regexp.MustCompile(`/archives/([CGD][A-Z0-9]{6,})`)
	nameOK    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// IsChannelID reports whether s looks like a Slack channel ID.
func IsChannelID(s string) bool { return channelID.MatchString(s) }

// ToForm turns a config route into a form.
func ToForm(r *config.Route) Form {
	h := r.Handler
	f := Form{
		OriginalName: r.Name, Name: r.Name, Channel: r.Channel, Type: h.Type, Folder: h.Cwd,
		Instructions: h.Instructions, Model: h.Model, PromptFile: h.Prompt, TimeoutSec: h.TimeoutSec,
		Commands: []string{}, OtherTools: []string{},
		Restricted: r.Restricted(), AllowedUsers: append([]string{}, r.AllowedUsers...),
	}
	f.Trigger, f.Keyword = r.Kind(), r.Pattern()
	if f.Trigger == "" {
		f.Trigger = config.KindStartsWith
	}
	if h.Type == config.TypeCommand && len(h.Command) > 0 {
		f.Program = h.Command[0]
		f.Args = JoinArgs(h.Command[1:])
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

// FromForm builds the raw route that will be written to the config. Fields
// the form does not edit (env_file, env, tools, extra_args, …) are kept from base.
func FromForm(f Form, base *config.Route) *config.Route {
	r := &config.Route{}
	if base != nil {
		copyRoute := *base
		r = &copyRoute
	}
	r.Name = strings.TrimSpace(f.Name)
	r.Channel = NormalizeChannel(f.Channel)
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
	r.AllowedUsers = nil
	if f.Restricted {
		r.AllowedUsers = cleanUsers(f.AllowedUsers)
	}
	h := &r.Handler
	h.Type = f.Type
	h.Cwd = TildePath(strings.TrimSpace(f.Folder))
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
		h.Command = append([]string{TildePath(strings.TrimSpace(f.Program))}, SplitArgs(f.Args)...)
	}
	return r
}

// Validate checks a form against the current config (raw, unexpanded).
func Validate(f Form, raw *config.Config) []FieldProblem {
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

	if ch := NormalizeChannel(f.Channel); ch == "" {
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

	if f.Restricted {
		users := cleanUsers(f.AllowedUsers)
		if len(users) == 0 {
			add("allowed_users", "required", "")
		}
		for _, u := range users {
			if !config.IsMemberID(u) {
				add("allowed_users", "bad_user", u)
				break
			}
		}
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

// cleanUsers trims member IDs and drops blanks and repeats, keeping the order.
func cleanUsers(ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// OpenToAll reports whether everyone in the channel can make an agent run
// commands or edit files with this rule ("risk": the rule page and
// sockdock_save_rule warn about it). It returns "commands", "edit" or "".
func OpenToAll(f Form) string {
	if f.Restricted || f.Type != config.TypeClaude {
		return ""
	}
	for _, c := range f.Commands {
		if strings.TrimSpace(c) != "" {
			return "commands"
		}
	}
	if f.AllowEdit {
		return "edit"
	}
	return ""
}

// AutoName returns an unused rule name ("rule-1", "rule-2", …).
func AutoName(raw *config.Config) string {
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

// NormalizeChannel accepts an ID, "#id" or a channel link and returns the ID.
func NormalizeChannel(s string) string {
	s = strings.TrimSpace(s)
	if m := linkID.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return strings.TrimPrefix(s, "#")
}

// TildePath shortens a path under the home directory to ~/… for readability.
func TildePath(p string) string {
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

// SplitArgs splits a command line on spaces, honoring double and single quotes.
func SplitArgs(s string) []string {
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

// JoinArgs is the inverse of SplitArgs.
func JoinArgs(args []string) string {
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
