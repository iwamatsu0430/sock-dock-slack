package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/iwamatsu0430/sock-dock-slack/assets"
	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/service"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// AppService is bound to the frontend. Every exported method becomes a
// JavaScript function in frontend/bindings.
type AppService struct {
	dirs    paths.Dirs
	version string
	client  *control.Client

	app     *application.App
	popover *application.WebviewWindow
	main    *application.WebviewWindow
}

func newAppService(dirs paths.Dirs, version string) *AppService {
	return &AppService{dirs: dirs, version: version, client: control.NewClient(dirs.SocketPath())}
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

// ---- gateway state -------------------------------------------------------

// Snapshot is what the UI shows: whether the gateway runs and its status.
type Snapshot struct {
	Running bool            `json:"running"`
	Error   string          `json:"error,omitempty"`
	Status  *gateway.Status `json:"status,omitempty"`
}

// GetSnapshot returns the current gateway status.
func (a *AppService) GetSnapshot() Snapshot {
	c, cancel := ctx()
	defer cancel()
	st, err := a.client.Status(c)
	if err != nil {
		return Snapshot{Running: false, Error: err.Error()}
	}
	return Snapshot{Running: true, Status: &st}
}

// Turns returns recent handler runs.
func (a *AppService) Turns(limit int) ([]store.Turn, error) {
	c, cancel := ctx()
	defer cancel()
	return a.client.Turns(c, limit)
}

// Threads returns recent threads.
func (a *AppService) Threads(limit int) ([]store.Thread, error) {
	c, cancel := ctx()
	defer cancel()
	return a.client.Threads(c, limit)
}

// Logs returns the last lines of the gateway log. It reads the file directly
// when the gateway is not running.
func (a *AppService) Logs(limit int) ([]string, error) {
	c, cancel := ctx()
	defer cancel()
	lines, err := a.client.Logs(c, limit)
	if errors.Is(err, control.ErrNotRunning) {
		b, ferr := os.ReadFile(a.dirs.LogFile())
		if ferr != nil {
			if errors.Is(ferr, os.ErrNotExist) {
				return []string{}, nil
			}
			return nil, ferr
		}
		all := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		return all[max(len(all)-limit, 0):], nil
	}
	return lines, err
}

// Reload asks the gateway to re-read its config.
func (a *AppService) Reload() error {
	c, cancel := ctx()
	defer cancel()
	return a.client.Reload(c)
}

// SetPaused pauses or resumes a route.
func (a *AppService) SetPaused(route string, paused bool) error {
	c, cancel := ctx()
	defer cancel()
	return a.client.SetPaused(c, route, paused)
}

// ---- config --------------------------------------------------------------

// ConfigFile is the config file and its content.
type ConfigFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// GetConfig reads the config file (directly, so it works while the gateway is stopped).
func (a *AppService) GetConfig() (ConfigFile, error) {
	p := a.dirs.ConfigFile
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return ConfigFile{Path: p, Content: assets.ConfigExample, Exists: false}, nil
	}
	if err != nil {
		return ConfigFile{}, err
	}
	return ConfigFile{Path: p, Content: string(b), Exists: true}, nil
}

// ValidateConfig checks config content without saving it.
func (a *AppService) ValidateConfig(content string) control.ValidateResult {
	if _, err := config.Parse([]byte(content), a.dirs.ConfigFile); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			return control.ValidateResult{Problems: ve.Problems}
		}
		return control.ValidateResult{Problems: []string{err.Error()}}
	}
	return control.ValidateResult{OK: true}
}

// SaveConfig validates and writes the config. When the gateway is running it
// goes through the gateway (which also reloads); otherwise it writes the file.
func (a *AppService) SaveConfig(content string) (control.ValidateResult, error) {
	if res := a.ValidateConfig(content); !res.OK {
		return res, nil
	}
	c, cancel := ctx()
	defer cancel()
	err := a.client.PutConfig(c, content)
	if errors.Is(err, control.ErrNotRunning) {
		if err := os.MkdirAll(filepath.Dir(a.dirs.ConfigFile), 0o700); err != nil {
			return control.ValidateResult{}, err
		}
		err = os.WriteFile(a.dirs.ConfigFile, []byte(content), 0o600)
	}
	if err != nil {
		var apiErr *control.APIError
		if errors.As(err, &apiErr) && len(apiErr.Problems) > 0 {
			return control.ValidateResult{Problems: apiErr.Problems}, nil
		}
		return control.ValidateResult{}, err
	}
	return control.ValidateResult{OK: true}, nil
}

// MatchInContent tests which route a message would start, using unsaved config content.
func (a *AppService) MatchInContent(content, channel, text string) (control.MatchResult, error) {
	cfg, err := config.Parse([]byte(content), a.dirs.ConfigFile)
	if err != nil {
		return control.MatchResult{}, err
	}
	return control.MatchConfig(cfg, control.MatchRequest{Channel: channel, Text: text}), nil
}

// ---- tokens --------------------------------------------------------------

// TokenInfo describes where a token comes from (never the value itself).
type TokenInfo struct {
	Name    string `json:"name"`
	Set     bool   `json:"set"`
	Source  string `json:"source"`
	Preview string `json:"preview"`
	Error   string `json:"error,omitempty"`
}

func (a *AppService) secretsFile() string {
	if cfg, err := config.Load(a.dirs.ConfigFile); err == nil {
		return cfg.Settings.SecretsFile
	}
	return ""
}

// TokenStatus reports both tokens.
func (a *AppService) TokenStatus() []TokenInfo {
	sf := a.secretsFile()
	var out []TokenInfo
	for _, n := range []string{secret.AppToken, secret.BotToken} {
		v, src, err := secret.Lookup(n, sf)
		ti := TokenInfo{Name: n, Set: v != "", Source: string(src)}
		if err != nil {
			ti.Error = err.Error()
		}
		if v != "" {
			ti.Preview = v[:min(5, len(v))] + "…" + v[max(len(v)-4, 0):]
		}
		out = append(out, ti)
	}
	return out
}

// SaveToken stores a token in the Keychain.
func (a *AppService) SaveToken(name, value string) error {
	value = strings.TrimSpace(value)
	switch name {
	case secret.AppToken:
		if !strings.HasPrefix(value, "xapp-") {
			return errors.New("the app-level token starts with xapp-")
		}
	case secret.BotToken:
		if !strings.HasPrefix(value, "xoxb-") {
			return errors.New("the bot token starts with xoxb-")
		}
	default:
		return fmt.Errorf("unknown token %q", name)
	}
	return secret.KeychainSet(name, value)
}

// TestConnection checks the bot token with auth.test.
func (a *AppService) TestConnection() (slackio.Identity, error) {
	app, bot, err := secret.Tokens(a.secretsFile())
	if err != nil {
		return slackio.Identity{}, err
	}
	c, cancel := ctx()
	defer cancel()
	return slackio.NewWebAPI(bot, app).AuthTest(c)
}

// Manifest returns the Slack app manifest.
func (a *AppService) Manifest() string { return assets.SlackAppManifest }

// CopyManifest puts the manifest on the clipboard.
func (a *AppService) CopyManifest() bool { return a.app.Clipboard.SetText(assets.SlackAppManifest) }

// ---- background service --------------------------------------------------

// ServiceInfo is the gateway service state.
type ServiceInfo struct {
	Installed bool   `json:"installed"`
	Detail    string `json:"detail"`
	Binary    string `json:"binary"`
}

// ServiceStatus reports whether the gateway service is installed and loaded.
func (a *AppService) ServiceStatus() ServiceInfo {
	detail, ok := service.Status()
	bin, _ := findSockdock()
	return ServiceInfo{Installed: ok, Detail: detail, Binary: bin}
}

// InstallService installs (or reinstalls) and starts the gateway service.
func (a *AppService) InstallService() error {
	if _, err := config.Load(a.dirs.ConfigFile); err != nil {
		return fmt.Errorf("fix the config first: %w", err)
	}
	bin, err := findSockdock()
	if err != nil {
		return err
	}
	_, err = service.Install(service.Spec{
		Binary:     service.StablePath(bin),
		ConfigFile: a.dirs.ConfigFile,
		LogDir:     a.dirs.LogDir,
		Path:       loginShellPath(),
	})
	return err
}

// UninstallService stops and removes the gateway service.
func (a *AppService) UninstallService() error { return service.Uninstall() }

// RestartService restarts the gateway service.
func (a *AppService) RestartService() error { return service.Restart() }

// ---- Claude Code ---------------------------------------------------------

// DetectClaude finds Claude Code executables worth suggesting for settings.claude_command.
func (a *AppService) DetectClaude() []string { return detectClaude() }

// ---- app preferences -----------------------------------------------------

// Prefs are GUI-only preferences.
type Prefs struct {
	Notifications bool `json:"notifications"`
}

func (a *AppService) prefsPath() string { return filepath.Join(a.dirs.DataDir, "gui.json") }

// GetPrefs reads GUI preferences.
func (a *AppService) GetPrefs() Prefs {
	p := Prefs{Notifications: true}
	if b, err := os.ReadFile(a.prefsPath()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

// SetPrefs writes GUI preferences.
func (a *AppService) SetPrefs(p Prefs) error {
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := os.MkdirAll(a.dirs.DataDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(a.prefsPath(), b, 0o600)
}

// LaunchAtLogin reports whether the GUI starts at login.
func (a *AppService) LaunchAtLogin() bool {
	on, _ := a.app.Autostart.IsEnabled()
	return on
}

// SetLaunchAtLogin turns starting the GUI at login on or off.
func (a *AppService) SetLaunchAtLogin(on bool) error {
	if on {
		return a.app.Autostart.Enable()
	}
	return a.app.Autostart.Disable()
}

// ---- windows and misc ----------------------------------------------------

// AppInfo is shown in the About section.
type AppInfo struct {
	Version    string `json:"version"`
	ConfigPath string `json:"config_path"`
	DataDir    string `json:"data_dir"`
	LogDir     string `json:"log_dir"`
}

// Info returns app paths and version.
func (a *AppService) Info() AppInfo {
	return AppInfo{Version: a.version, ConfigPath: a.dirs.ConfigFile, DataDir: a.dirs.DataDir, LogDir: a.dirs.LogDir}
}

// OpenWindow shows the main window on a tab.
func (a *AppService) OpenWindow(tab string) {
	if a.popover != nil {
		a.popover.Hide()
	}
	if a.main == nil {
		return
	}
	a.app.Event.Emit(evNavigate, tab)
	a.main.Show()
	a.main.Focus()
}

// OpenURL opens a link in the default browser.
func (a *AppService) OpenURL(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return errors.New("only https links can be opened")
	}
	return a.app.Browser.OpenURL(url)
}

// RevealLogs opens the log folder in Finder.
func (a *AppService) RevealLogs() error { return exec.Command("open", a.dirs.LogDir).Run() }

// Quit exits the GUI (the gateway keeps running).
func (a *AppService) Quit() { a.app.Quit() }
