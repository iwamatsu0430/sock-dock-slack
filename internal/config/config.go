// Package config loads, expands and validates sockdock's config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/iwamatsu0430/sock-dock-slack/internal/locale"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

// Handler types.
const (
	TypeCommand = "command"
	TypeClaude  = "claude"
)

// Config is the whole config file.
type Config struct {
	Settings Settings `yaml:"settings"`
	Routes   []*Route `yaml:"routes"`

	// Path is the file this config was loaded from.
	Path string `yaml:"-"`
	// Broken lists routes left out by ParseLenient because of problems.
	Broken []BrokenRoute `yaml:"-"`
}

// BrokenRoute is a route that could not be used, and why.
type BrokenRoute struct {
	Name     string   `json:"name"`
	Problems []string `json:"problems"`
}

// Settings are gateway-wide options.
type Settings struct {
	MaxConcurrency int        `yaml:"max_concurrency"`
	ThreadTTLDays  int        `yaml:"thread_ttl_days"`
	AckReaction    string     `yaml:"ack_reaction"`
	ClaudeCommand  StringList `yaml:"claude_command"`
	Path           []string   `yaml:"path"`
	SecretsFile    string     `yaml:"secrets_file"`
	// Language of the messages posted to Slack: "auto" (default, follows
	// the system), "en" or "ja".
	Language string   `yaml:"language"`
	Messages Messages `yaml:"messages"`

	// ResolvedLanguage is Language after resolving "auto".
	ResolvedLanguage string `yaml:"-"`
}

// Messages are the canned replies posted by the gateway. Placeholders:
// {route}, {timeout_sec}.
type Messages struct {
	Timeout      string `yaml:"timeout"`
	Error        string `yaml:"error"`
	Stopped      string `yaml:"stopped"`
	Interrupted  string `yaml:"interrupted"`
	RouteMissing string `yaml:"route_missing"`
	RoutePaused  string `yaml:"route_paused"`
	ResumeFailed string `yaml:"resume_failed"`
}

// Route binds a condition on top-level posts in a channel to a handler. The
// condition is exactly one of StartsWith, Contains, Match (a regex) and
// AllPosts (see match.go).
type Route struct {
	Name       string `yaml:"name"`
	Channel    string `yaml:"channel"`
	StartsWith string `yaml:"starts_with"`
	Contains   string `yaml:"contains"`
	Match      string `yaml:"match"`
	AllPosts   bool   `yaml:"all_posts"`
	// AllowedUsers are the Slack member IDs who may run the route ("実行できる
	// 人"). Empty means everyone in the channel (docs/design.md §3.4).
	AllowedUsers []string `yaml:"allowed_users"`
	Handler      Handler  `yaml:"handler"`

	re *regexp.Regexp
	// Env holds the values read from Handler.EnvFile plus Handler.Env.
	Env map[string]string `yaml:"-"`
}

// Handler describes what runs for a route.
type Handler struct {
	Type       string            `yaml:"type"`
	Cwd        string            `yaml:"cwd"`
	EnvFile    string            `yaml:"env_file"`
	Env        map[string]string `yaml:"env"`
	Command    StringList        `yaml:"command"`
	TimeoutSec int               `yaml:"timeout_sec"`

	// claude only
	Prompt string `yaml:"prompt"`
	// Instructions is inline text appended to the system prompt (like
	// Prompt, but kept in the config; the app's route form writes it).
	Instructions string   `yaml:"instructions"`
	AllowedTools []string `yaml:"allowed_tools"`
	Tools        []string `yaml:"tools"`
	AllowMCP     bool     `yaml:"allow_mcp"`
	Model        string   `yaml:"model"`
	ExtraArgs    []string `yaml:"extra_args"`
}

// StringList accepts either a YAML string or a list of strings.
type StringList []string

// UnmarshalYAML implements yaml.InterfaceUnmarshaler.
func (s *StringList) UnmarshalYAML(unmarshal func(any) error) error {
	var one string
	if err := unmarshal(&one); err == nil {
		*s = StringList{one}
		return nil
	}
	var many []string
	if err := unmarshal(&many); err != nil {
		return errors.New("expected a string or a list of strings")
	}
	*s = many
	return nil
}

// Defaults.
const (
	DefaultMaxConcurrency    = 3
	DefaultThreadTTLDays     = 30
	DefaultAckReaction       = "eyes"
	DefaultCommandTimeoutSec = 300
	DefaultClaudeTimeoutSec  = 600
)

// DefaultMessages are the English canned replies.
var DefaultMessages = defaultMessages["en"]

// defaultMessages are used when a message is not set in the config, per language.
var defaultMessages = map[string]Messages{
	"en": {
		Timeout:      "⏱ Stopped because it ran past the time limit ({timeout_sec} seconds). Try sending it in smaller parts.",
		Error:        "⚠️ This failed. The reason is shown in SockDock on the Mac.",
		Stopped:      "⏹ Stopped.",
		Interrupted:  "⚠️ Stopped because SockDock quit while this was running. Please send it again.",
		RouteMissing: "⚠️ The rule for this thread has been deleted.",
		RoutePaused:  "⏸ The rule for this thread is turned off.",
		ResumeFailed: "ℹ️ Couldn't pick up the previous conversation, so this started a new one.",
	},
	"ja": {
		Timeout:      "⏱ 上限時間（{timeout_sec} 秒）を超えたため停止しました。内容を分けて送ってください。",
		Error:        "⚠️ 処理に失敗しました。理由は Mac の SockDock で確認できます。",
		Stopped:      "⏹ 処理を停止しました。",
		Interrupted:  "⚠️ 処理の途中で SockDock が終了したため、停止しました。もう一度送ってください。",
		RouteMissing: "⚠️ このスレッドのルールは削除されています。",
		RoutePaused:  "⏸ このスレッドのルールはオフになっています。",
		ResumeFailed: "ℹ️ 前の会話を引き継げなかったため、新しい会話として始めました。",
	},
}

// DefaultMessagesFor returns the canned replies for a supported language
// (English for anything else).
func DefaultMessagesFor(lang string) Messages {
	if m, ok := defaultMessages[lang]; ok {
		return m
	}
	return DefaultMessages
}

var (
	varRef   = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	nameRule = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	memberID = regexp.MustCompile(`^[UW][A-Z0-9]{2,}$`)
)

// IsMemberID reports whether s looks like a Slack member ID (U… or W…).
func IsMemberID(s string) bool { return memberID.MatchString(s) }

// ValidationError lists every problem found in a config.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid config:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Load reads, expands and validates the config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

// Parse is Load for in-memory content. path is used for error messages and
// for resolving a relative secrets_file.
func Parse(data []byte, path string) (*Config, error) {
	c, global, broken, err := parse(data, path)
	if err != nil {
		return nil, err
	}
	for _, b := range broken {
		global = append(global, b.Problems...)
	}
	if len(global) > 0 {
		return nil, &ValidationError{Problems: global}
	}
	return c, nil
}

// LoadLenient is Load, but leaves out routes with problems (listed in
// Config.Broken) instead of failing, so one bad route does not stop the rest.
func LoadLenient(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseLenient(data, path)
}

// ParseLenient is LoadLenient for in-memory content. It fails only on
// problems outside routes (YAML syntax, settings).
func ParseLenient(data []byte, path string) (*Config, error) {
	c, global, broken, err := parse(data, path)
	if err != nil {
		return nil, err
	}
	if len(global) > 0 {
		return nil, &ValidationError{Problems: global}
	}
	if len(broken) > 0 {
		bad := map[string]bool{}
		for _, b := range broken {
			bad[b.Name] = true
		}
		kept := c.Routes[:0:0]
		for _, r := range c.Routes {
			if !bad[r.Name] {
				kept = append(kept, r)
			}
		}
		c.Routes, c.Broken = kept, broken
	}
	return c, nil
}

func parse(data []byte, path string) (c *Config, global []string, broken []BrokenRoute, err error) {
	c = &Config{}
	if err := yaml.UnmarshalWithOptions(data, c, yaml.DisallowUnknownField()); err != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.Path = path
	for _, r := range c.Routes {
		r.upgradeLegacy()
	}
	c.applyDefaults()
	global, broken = c.prepare(os.LookupEnv)
	return c, global, broken, nil
}

func (c *Config) applyDefaults() {
	s := &c.Settings
	if s.MaxConcurrency <= 0 {
		s.MaxConcurrency = DefaultMaxConcurrency
	}
	if s.ThreadTTLDays <= 0 {
		s.ThreadTTLDays = DefaultThreadTTLDays
	}
	if s.AckReaction == "" {
		s.AckReaction = DefaultAckReaction
	}
	if len(s.ClaudeCommand) == 0 {
		s.ClaudeCommand = StringList{"claude"}
	}
	s.ResolvedLanguage = locale.Resolve(s.Language)
	m, d := &s.Messages, DefaultMessagesFor(s.ResolvedLanguage)
	for _, p := range []struct {
		v   *string
		def string
	}{
		{&m.Timeout, d.Timeout}, {&m.Error, d.Error}, {&m.Stopped, d.Stopped}, {&m.Interrupted, d.Interrupted},
		{&m.RouteMissing, d.RouteMissing}, {&m.RoutePaused, d.RoutePaused}, {&m.ResumeFailed, d.ResumeFailed},
	} {
		if *p.v == "" {
			*p.v = p.def
		}
	}
	for _, r := range c.Routes {
		if r.Handler.TimeoutSec <= 0 {
			if r.Handler.Type == TypeClaude {
				r.Handler.TimeoutSec = DefaultClaudeTimeoutSec
			} else {
				r.Handler.TimeoutSec = DefaultCommandTimeoutSec
			}
		}
	}
}

// prepare expands ${VAR} references and validates every route. It returns
// problems outside routes and, per route, the routes with problems.
func (c *Config) prepare(lookupEnv func(string) (string, bool)) (global []string, broken []BrokenRoute) {
	add := func(format string, a ...any) { global = append(global, fmt.Sprintf(format, a...)) }

	if !locale.Valid(c.Settings.Language) {
		add("settings.language: %q is not supported (use auto, %s)", c.Settings.Language, strings.Join(locale.Supported, ", "))
	}

	// Settings may only reference the process environment.
	envOnly := func(s string) string {
		out, missing := expand(s, nil, lookupEnv)
		for _, m := range missing {
			add("settings: ${%s} is not set", m)
		}
		return out
	}
	for i := range c.Settings.ClaudeCommand {
		c.Settings.ClaudeCommand[i] = paths.ExpandHome(envOnly(c.Settings.ClaudeCommand[i]))
	}
	for i := range c.Settings.Path {
		c.Settings.Path[i] = paths.ExpandHome(envOnly(c.Settings.Path[i]))
	}
	if c.Settings.SecretsFile != "" {
		c.Settings.SecretsFile = c.resolveRelative(paths.ExpandHome(envOnly(c.Settings.SecretsFile)))
	}

	seen := map[string]bool{}
	for i, r := range c.Routes {
		var probs []string
		add := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }
		label := fmt.Sprintf("routes[%d]", i)
		if r.Name != "" {
			label = fmt.Sprintf("route %q", r.Name)
		}
		switch {
		case r.Name == "":
			add("%s: name is required", label)
		case !nameRule.MatchString(r.Name):
			add("%s: name may contain only letters, digits, '.', '_' and '-'", label)
		case seen[r.Name]:
			add("%s: duplicate name", label)
		}
		seen[r.Name] = true

		h := &r.Handler
		// cwd first: env_file is relative to it.
		cwd, missing := expand(h.Cwd, nil, lookupEnv)
		for _, m := range missing {
			add("%s: cwd: ${%s} is not set", label, m)
		}
		h.Cwd = paths.ExpandHome(cwd)
		if h.Cwd == "" {
			add("%s: handler.cwd is required", label)
		} else if !filepath.IsAbs(h.Cwd) {
			add("%s: handler.cwd must be an absolute path (got %q)", label, h.Cwd)
		} else if st, err := os.Stat(h.Cwd); err != nil || !st.IsDir() {
			add("%s: handler.cwd %q is not a directory", label, h.Cwd)
		}

		r.Env = map[string]string{}
		if h.EnvFile != "" {
			p := paths.ExpandHome(h.EnvFile)
			if !filepath.IsAbs(p) {
				p = filepath.Join(h.Cwd, p)
			}
			h.EnvFile = p
			vals, err := ReadDotenv(p)
			if err != nil {
				add("%s: env_file: %v", label, err)
			}
			for k, v := range vals {
				r.Env[k] = v
			}
		}
		for k, v := range h.Env {
			r.Env[k] = v
		}

		ex := func(field, s string) string {
			out, missing := expand(s, r.Env, lookupEnv)
			for _, m := range missing {
				add("%s: %s: ${%s} is not set", label, field, m)
			}
			return out
		}
		r.Channel = ex("channel", r.Channel)
		r.StartsWith = ex("starts_with", r.StartsWith)
		r.Contains = ex("contains", r.Contains)
		r.Match = ex("match", r.Match)
		h.Prompt = ex("prompt", h.Prompt)
		h.Model = ex("model", h.Model)
		for j := range h.Command {
			h.Command[j] = paths.ExpandHome(ex("command", h.Command[j]))
		}
		for j := range h.ExtraArgs {
			h.ExtraArgs[j] = ex("extra_args", h.ExtraArgs[j])
		}
		for k, v := range h.Env {
			r.Env[k] = ex("env."+k, v)
		}

		if r.Channel == "" {
			add("%s: channel is required", label)
		}
		if p := r.conditionProblem(); p != "" {
			add("%s: %s", label, p)
		}
		if r.AllowedUsers != nil && len(r.AllowedUsers) == 0 {
			add("%s: allowed_users is empty; list member IDs, or remove the key to let everyone in the channel run it", label)
		}
		for _, u := range r.AllowedUsers {
			if !IsMemberID(u) {
				add("%s: allowed_users: %q is not a Slack member ID (like U0123ABCD)", label, u)
			}
		}

		switch h.Type {
		case TypeCommand:
			if len(h.Command) == 0 {
				add("%s: handler.command is required for type command", label)
			} else if err := c.checkExecutable(h.Command[0], h.Cwd); err != nil {
				add("%s: handler.command: %v", label, err)
			}
		case TypeClaude:
			cmd := c.ClaudeCommandFor(r)
			if err := c.checkExecutable(cmd[0], h.Cwd); err != nil {
				add("%s: claude command: %v", label, err)
			}
			if h.Prompt != "" {
				p := paths.ExpandHome(h.Prompt)
				if !filepath.IsAbs(p) {
					p = filepath.Join(h.Cwd, p)
				}
				h.Prompt = p
				if _, err := os.Stat(p); err != nil {
					add("%s: handler.prompt: %v", label, err)
				}
			}
		case "":
			add("%s: handler.type is required (command or claude)", label)
		default:
			add("%s: unknown handler.type %q (command or claude)", label, h.Type)
		}
		if len(probs) > 0 {
			name := r.Name
			if name == "" || countName(c.Routes, name) > 1 {
				// Unnamed or duplicate: keep it distinct so only this one is left out.
				name = label
				r.Name = label
			}
			broken = append(broken, BrokenRoute{Name: name, Problems: probs})
		}
	}
	return global, broken
}

func countName(routes []*Route, name string) int {
	n := 0
	for _, r := range routes {
		if r.Name == name {
			n++
		}
	}
	return n
}

// ClaudeCommandFor returns the command (and leading args) used to start
// Claude Code for a route.
func (c *Config) ClaudeCommandFor(r *Route) []string {
	if len(r.Handler.Command) > 0 {
		return r.Handler.Command
	}
	return c.Settings.ClaudeCommand
}

// SearchPath returns PATH entries for handlers: settings.path first, then the
// process PATH.
func (c *Config) SearchPath() string {
	parts := append([]string{}, c.Settings.Path...)
	if p := os.Getenv("PATH"); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func (c *Config) checkExecutable(name, cwd string) error {
	if name == "" {
		return errors.New("empty command")
	}
	if strings.ContainsRune(name, '/') {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		if st.IsDir() || st.Mode()&0o111 == 0 {
			return fmt.Errorf("%s is not executable", p)
		}
		return nil
	}
	if _, err := LookPath(name, c.SearchPath()); err != nil {
		return fmt.Errorf("%q not found in PATH (set an absolute path, or add its directory to settings.path)", name)
	}
	return nil
}

// LookPath is exec.LookPath against an explicit PATH value.
func LookPath(name, pathEnv string) (string, error) {
	if strings.ContainsRune(name, '/') {
		return exec.LookPath(name)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

func (c *Config) resolveRelative(p string) string {
	if p == "" || filepath.IsAbs(p) || c.Path == "" {
		return p
	}
	return filepath.Join(filepath.Dir(c.Path), p)
}

// Route returns the route with the given name, or nil.
func (c *Config) Route(name string) *Route {
	for _, r := range c.Routes {
		if r.Name == name {
			return r
		}
	}
	return nil
}

// expand replaces ${VAR} using local first, then lookupEnv. It returns the
// names that could not be resolved.
func expand(s string, local map[string]string, lookupEnv func(string) (string, bool)) (string, []string) {
	var missing []string
	out := varRef.ReplaceAllStringFunc(s, func(ref string) string {
		name := varRef.FindStringSubmatch(ref)[1]
		if v, ok := local[name]; ok {
			return v
		}
		if v, ok := lookupEnv(name); ok {
			return v
		}
		missing = append(missing, name)
		return ref
	})
	return out, missing
}

// Render fills {route} and {timeout_sec} in a canned message.
func Render(tmpl, route string, timeoutSec int) string {
	return strings.NewReplacer("{route}", route, "{timeout_sec}", fmt.Sprint(timeoutSec)).Replace(tmpl)
}

// prepareMatchOnly checks route conditions without other validation (tests).
func (c *Config) prepareMatchOnly() error {
	for _, r := range c.Routes {
		r.upgradeLegacy()
		if p := r.conditionProblem(); p != "" {
			return errors.New(p)
		}
	}
	return nil
}
