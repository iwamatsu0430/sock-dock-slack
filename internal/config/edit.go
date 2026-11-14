package config

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
)

// MinimalConfig is written when no config exists yet (by the menu bar app):
// a gateway with no routes is valid and connects to Slack.
const MinimalConfig = `# SockDock for Slack config. Rules (routes) are usually edited in the app.
settings:
  language: auto

routes: []
`

// ParseRaw reads the config without expanding ${VAR}, resolving paths or
// validating, so it can be edited and written back as the user wrote it.
func ParseRaw(data []byte) (*Config, error) {
	var c Config
	if err := yaml.UnmarshalWithOptions(data, &c, yaml.DisallowUnknownField()); err != nil {
		return nil, err
	}
	for _, r := range c.Routes {
		r.upgradeLegacy()
	}
	return &c, nil
}

// routeOut and handlerOut control how routes are written: empty fields are left out.
type routeOut struct {
	Name        string `yaml:"name"`
	Channel     string `yaml:"channel"`
	StartsWith  string `yaml:"starts_with,omitempty"`
	Contains    string `yaml:"contains,omitempty"`
	Match       string `yaml:"match,omitempty"`
	AllPosts    bool   `yaml:"all_posts,omitempty"`
	MentionOnly bool   `yaml:"mention_only,omitempty"`
	// AllowedUsers is written in flow style: allowed_users: [U0123ABCD].
	AllowedUsers []string   `yaml:"allowed_users,omitempty,flow"`
	Handler      handlerOut `yaml:"handler"`
}

type handlerOut struct {
	Type         string            `yaml:"type"`
	Cwd          string            `yaml:"cwd"`
	EnvFile      string            `yaml:"env_file,omitempty"`
	Env          map[string]string `yaml:"env,omitempty"`
	Command      []string          `yaml:"command,omitempty,flow"`
	Prompt       string            `yaml:"prompt,omitempty"`
	Instructions string            `yaml:"instructions,omitempty"`
	AllowedTools []string          `yaml:"allowed_tools,omitempty,flow"`
	Tools        []string          `yaml:"tools,omitempty,flow"`
	AllowMCP     bool              `yaml:"allow_mcp,omitempty"`
	AllowWeb     bool              `yaml:"allow_web,omitempty"`
	AllowEdit    bool              `yaml:"allow_edit,omitempty"`
	Model        string            `yaml:"model,omitempty"`
	ExtraArgs    []string          `yaml:"extra_args,omitempty,flow"`
	TimeoutSec   int               `yaml:"timeout_sec,omitempty"`
}

// RenderRoutes writes a top-level `routes:` block.
func RenderRoutes(routes []*Route) ([]byte, error) {
	if len(routes) == 0 {
		return []byte("routes: []\n"), nil
	}
	out := make([]routeOut, 0, len(routes))
	for _, r := range routes {
		h := r.Handler
		out = append(out, routeOut{
			Name: r.Name, Channel: r.Channel, StartsWith: r.StartsWith, Contains: r.Contains, Match: r.Match, AllPosts: r.AllPosts,
			MentionOnly: r.MentionOnly, AllowedUsers: r.AllowedUsers,
			Handler: handlerOut{
				Type: h.Type, Cwd: h.Cwd, EnvFile: h.EnvFile, Env: h.Env, Command: h.Command,
				Prompt: h.Prompt, Instructions: h.Instructions, AllowedTools: h.AllowedTools, Tools: h.Tools,
				AllowMCP: h.AllowMCP, AllowWeb: h.AllowWeb, AllowEdit: h.AllowEdit, Model: h.Model, ExtraArgs: h.ExtraArgs, TimeoutSec: h.TimeoutSec,
			},
		})
	}
	body, err := yaml.MarshalWithOptions(map[string]any{"routes": out}, yaml.Indent(2), yaml.IndentSequence(true), yaml.UseLiteralStyleIfMultiline(true))
	if err != nil {
		return nil, err
	}
	return body, nil
}

var topLevelKey = regexp.MustCompile(`^[A-Za-z_][\w-]*\s*:`)

// ReplaceRoutes swaps the top-level `routes:` block of a config for the given
// routes and keeps everything else (settings, comments) as it was. Comments
// inside the old routes block are not kept.
func ReplaceRoutes(data []byte, routes []*Route) ([]byte, error) {
	block, err := RenderRoutes(routes)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "routes:") {
			start = i
			break
		}
	}
	var b bytes.Buffer
	if start == -1 {
		b.WriteString(strings.Join(lines, "\n"))
		b.WriteString("\n\n")
		b.Write(block)
		return b.Bytes(), nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if topLevelKey.MatchString(lines[i]) {
			end = i
			break
		}
	}
	// Leave trailing blank lines and comments above the next key in place.
	for end > start+1 && (strings.TrimSpace(lines[end-1]) == "" || strings.HasPrefix(lines[end-1], "#")) {
		end--
	}
	b.WriteString(strings.Join(lines[:start], "\n"))
	if start > 0 {
		b.WriteString("\n")
	}
	b.Write(block)
	if end < len(lines) {
		b.WriteString("\n")
		b.WriteString(strings.Join(lines[end:], "\n"))
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// SetSetting sets `settings.<key>: <value>` (value is written as a YAML
// scalar), adding the settings block or the key when missing.
func SetSetting(data []byte, key, value string) ([]byte, error) {
	return SetSettingValue(data, key, value)
}

// SetSettingValue is SetSetting for any scalar (a bool is written as true/false).
func SetSettingValue(data []byte, key string, value any) ([]byte, error) {
	scalar, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	entry := fmt.Sprintf("  %s: %s", key, strings.TrimSpace(string(scalar)))
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "settings:") {
			start = i
			break
		}
	}
	if start == -1 {
		return []byte("settings:\n" + entry + "\n\n" + strings.Join(lines, "\n") + "\n"), nil
	}
	if strings.TrimSpace(strings.TrimPrefix(lines[start], "settings:")) != "" {
		return nil, fmt.Errorf("cannot edit an inline settings block; edit the config file instead")
	}
	keyRe := regexp.MustCompile(`^\s{2}` + regexp.QuoteMeta(key) + `\s*:`)
	for i := start + 1; i < len(lines) && !topLevelKey.MatchString(lines[i]); i++ {
		if keyRe.MatchString(lines[i]) {
			lines[i] = entry
			return []byte(strings.Join(lines, "\n") + "\n"), nil
		}
	}
	lines = append(lines[:start+1], append([]string{entry}, lines[start+1:]...)...)
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

// DeleteSetting removes `settings.<key>` when present.
func DeleteSetting(data []byte, key string) []byte {
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "settings:") {
			start = i
			break
		}
	}
	if start == -1 {
		return data
	}
	keyRe := regexp.MustCompile(`^\s{2}` + regexp.QuoteMeta(key) + `\s*:`)
	for i := start + 1; i < len(lines) && !topLevelKey.MatchString(lines[i]); i++ {
		if keyRe.MatchString(lines[i]) {
			lines = append(lines[:i], lines[i+1:]...)
			return []byte(strings.Join(lines, "\n") + "\n")
		}
	}
	return data
}

// KeywordMatch builds the regex older versions of the app wrote for "a
// message that starts with <keyword>" (read back as starts_with).
func KeywordMatch(keyword string) string {
	return `(?s)^\s*` + regexp.QuoteMeta(keyword) + `(?:\s+(?P<text>.*))?$`
}

var keywordMatchRe = regexp.MustCompile(`^\(\?s\)\^\\s\*(.+)\(\?:\\s\+\(\?P<text>\.\*\)\)\?\$$`)

// KeywordFromMatch returns the keyword if match was built by KeywordMatch.
func KeywordFromMatch(match string) (string, bool) {
	m := keywordMatchRe.FindStringSubmatch(match)
	if m == nil {
		return "", false
	}
	// Undo QuoteMeta.
	kw := regexp.MustCompile(`\\(.)`).ReplaceAllString(m[1], "$1")
	if KeywordMatch(kw) != match {
		return "", false
	}
	return kw, true
}
