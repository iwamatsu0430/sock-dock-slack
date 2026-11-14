package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/iwamatsu0430/sock-dock-slack/assets"
	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/control"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/locale"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
	"github.com/iwamatsu0430/sock-dock-slack/internal/slackio"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// AppService is bound to the frontend. Every exported method becomes a
// JavaScript function in frontend/bindings.
type AppService struct {
	dirs    paths.Dirs
	version string
	client  *control.Client

	app  *application.App
	main *application.WebviewWindow

	// onLocale is called after the UI language changes (to relabel native menus).
	onLocale func()

	// emb runs the gateway inside this app.
	emb *embedded

	// channels caches channel names (id → channel) for labels.
	chMu     sync.Mutex
	channels map[string]Channel

	// members keeps the workspace's people for the "実行できる人" field.
	members *slackio.MemberDirectory
}

func newAppService(dirs paths.Dirs, version string) *AppService {
	a := &AppService{
		dirs:     dirs,
		version:  version,
		client:   control.NewClient(dirs.SocketPath()),
		emb:      newEmbedded(dirs, version),
		channels: map[string]Channel{},
	}
	a.members = &slackio.MemberDirectory{Fetch: a.fetchMembers}
	return a
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
	// Gateway says whether the gateway runs inside this app, elsewhere, or why not.
	Gateway GatewayState `json:"gateway"`
}

// GetSnapshot returns the current gateway status.
func (a *AppService) GetSnapshot() Snapshot {
	c, cancel := ctx()
	defer cancel()
	st, err := a.client.Status(c)
	if err != nil {
		return Snapshot{Running: false, Error: err.Error(), Gateway: a.emb.State()}
	}
	return Snapshot{Running: true, Status: &st, Gateway: a.emb.State()}
}

// startAfterSetup (re)starts the embedded gateway after something it depends
// on changed, unless another sockdock runs.
func (a *AppService) startAfterSetup() {
	switch a.emb.State().State {
	case GatewayRunning:
		a.emb.Restart()
	case GatewaySetup, GatewayError, GatewayStopped:
		a.emb.Start()
	}
}

// SetPaused turns a rule off (paused) or on.
func (a *AppService) SetPaused(route string, paused bool) error {
	c, cancel := ctx()
	defer cancel()
	return a.client.SetPaused(c, route, paused)
}

// RecentTurns returns recent runs of all rules.
func (a *AppService) RecentTurns(limit int) []store.Turn {
	c, cancel := ctx()
	defer cancel()
	out, err := a.client.Turns(c, limit)
	if err != nil {
		return []store.Turn{}
	}
	return out
}

// RuleTurns returns the recent runs of one rule.
func (a *AppService) RuleTurns(route string) []store.Turn {
	c, cancel := ctx()
	defer cancel()
	out, err := a.client.RouteTurns(c, route, 10)
	if err != nil || out == nil {
		return []store.Turn{}
	}
	return out
}

// StopTurn stops a running run.
func (a *AppService) StopTurn(id int64) error {
	c, cancel := ctx()
	defer cancel()
	return a.client.StopTurn(c, id)
}

// ---- config --------------------------------------------------------------

// saveConfig validates and writes the config. When the gateway is running it
// goes through the gateway (which also reloads); otherwise it writes the file.
// A rule with problems does not block saving: the gateway leaves it out.
func (a *AppService) saveConfig(content string) (control.ValidateResult, error) {
	if _, err := config.ParseLenient([]byte(content), a.dirs.ConfigFile); err != nil {
		var ve *config.ValidationError
		if errors.As(err, &ve) {
			return control.ValidateResult{Problems: ve.Problems}, nil
		}
		return control.ValidateResult{Problems: []string{err.Error()}}, nil
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
	if st := a.emb.State().State; st == GatewaySetup || st == GatewayError {
		go a.emb.Start()
	}
	return control.ValidateResult{OK: true}, nil
}

// OpenConfigFile opens the config file in the user's text editor.
func (a *AppService) OpenConfigFile() error {
	if err := a.ensureConfig(); err != nil {
		return err
	}
	return exec.Command("open", "-t", a.dirs.ConfigFile).Run()
}

func (a *AppService) secretsFile() string {
	if cfg, err := config.LoadLenient(a.dirs.ConfigFile); err == nil {
		return cfg.Settings.SecretsFile
	}
	return ""
}

// ---- Slack connection ----------------------------------------------------

// TokenProblem says what is wrong with a token. Code is "format",
// "missing_scope", "invalid_auth" (and other Slack error codes) or
// "network"; Detail carries Slack's text.
type TokenProblem struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

// ConnectResult is the outcome of ConnectSlack.
type ConnectResult struct {
	OK   bool          `json:"ok"`
	Team string        `json:"team,omitempty"`
	App  *TokenProblem `json:"app,omitempty"`
	Bot  *TokenProblem `json:"bot,omitempty"`
	// Start is set when the tokens worked and were saved, but SockDock did not
	// get connected to Slack in time (Detail says why, when known).
	Start *TokenProblem `json:"start,omitempty"`
}

// connectWait is how long ConnectSlack waits for the gateway to connect.
const connectWait = 20 * time.Second

// slackCodes are Slack error codes worth showing as they are.
var slackCodes = []string{"missing_scope", "invalid_auth", "not_authed", "token_revoked", "token_expired", "account_inactive", "not_allowed_token_type"}

func tokenProblem(err error) *TokenProblem {
	msg := err.Error()
	for _, c := range slackCodes {
		if strings.Contains(msg, c) {
			return &TokenProblem{Code: c}
		}
	}
	return &TokenProblem{Code: "network", Detail: msg}
}

// ConnectSlack checks both tokens with Slack and, only if both work, saves
// them in the Keychain and (re)starts the gateway. Nothing is saved before.
func (a *AppService) ConnectSlack(appToken, botToken string) ConnectResult {
	appToken, botToken = strings.TrimSpace(appToken), strings.TrimSpace(botToken)
	var res ConnectResult
	if !strings.HasPrefix(appToken, "xapp-") {
		res.App = &TokenProblem{Code: "format"}
	}
	if !strings.HasPrefix(botToken, "xoxb-") {
		res.Bot = &TokenProblem{Code: "format"}
	}
	if res.App != nil || res.Bot != nil {
		return res
	}
	c, cancel := ctx()
	defer cancel()
	if err := slackio.TestAppToken(c, appToken); err != nil {
		res.App = tokenProblem(err)
	}
	id, scopes, err := slackio.AuthTestScopes(c, botToken)
	switch {
	case err != nil:
		res.Bot = tokenProblem(err)
	case scopes != nil:
		// An existing Slack app may lack scopes; name the required ones that
		// are missing. The optional ones only enable the channel list.
		if missing := slackio.MissingScopes(scopes, slackio.RequiredBotScopes); len(missing) > 0 {
			res.Bot = &TokenProblem{Code: "missing_bot_scopes", Detail: strings.Join(missing, " ")}
		}
	}
	if res.App != nil || res.Bot != nil {
		return res
	}
	if err := secret.KeychainSet(secret.AppToken, appToken); err != nil {
		res.App = &TokenProblem{Code: "keychain", Detail: err.Error()}
		return res
	}
	if err := secret.KeychainSet(secret.BotToken, botToken); err != nil {
		res.Bot = &TokenProblem{Code: "keychain", Detail: err.Error()}
		return res
	}
	a.chMu.Lock()
	a.channels = map[string]Channel{}
	a.chMu.Unlock()
	a.members.Refresh()
	a.startAfterSetup()
	// Report success only once SockDock is really connected, so setup does
	// not move on while nothing works.
	if p := a.waitConnected(connectWait); p != nil {
		res.Start = p
		return res
	}
	res.OK, res.Team = true, id.Team
	return res
}

// waitConnected waits until the gateway is connected to Slack, or says why not.
func (a *AppService) waitConnected(timeout time.Duration) *TokenProblem {
	deadline := time.Now().Add(timeout)
	for {
		snap := a.GetSnapshot()
		if snap.Running && snap.Status != nil && snap.Status.Connected {
			return nil
		}
		switch st := snap.Gateway; st.State {
		case GatewayError, GatewaySetup:
			return &TokenProblem{Code: "start", Detail: st.Error}
		case GatewayExternal:
			return &TokenProblem{Code: "external"}
		}
		if time.Now().After(deadline) {
			detail := ""
			if snap.Status != nil {
				detail = snap.Status.LastError
			}
			return &TokenProblem{Code: "timeout", Detail: detail}
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// DisconnectSlack removes the tokens from the Keychain and stops the gateway.
func (a *AppService) DisconnectSlack() error {
	a.emb.Stop(30 * time.Second)
	_ = secret.KeychainDelete(secret.AppToken)
	_ = secret.KeychainDelete(secret.BotToken)
	a.members.Refresh()
	sf := a.secretsFile()
	app, _, _ := secret.Lookup(secret.AppToken, sf)
	bot, _, _ := secret.Lookup(secret.BotToken, sf)
	defer a.emb.Start() // reports "setup" without tokens
	if app != "" || bot != "" {
		return errors.New(a.tr("err.disconnectEnv"))
	}
	return nil
}

// hasTokens reports whether both tokens are available.
func (a *AppService) hasTokens() bool {
	sf := a.secretsFile()
	app, _, _ := secret.Lookup(secret.AppToken, sf)
	bot, _, _ := secret.Lookup(secret.BotToken, sf)
	return app != "" && bot != ""
}

// slackSettingsSnippet is the part of the manifest an existing Slack app
// needs; it is meant to be added to that app's App Manifest, not to replace it.
const slackSettingsSnippet = `features:
  app_home:
    messages_tab_enabled: true
    messages_tab_read_only_enabled: false
oauth_config:
  scopes:
    bot:
      - channels:history
      - groups:history
      - chat:write
      - reactions:write
      - channels:read
      - groups:read
      - users:read
      - im:history
      - files:read
settings:
  event_subscriptions:
    bot_events:
      - message.channels
      - message.groups
      - message.im
  socket_mode_enabled: true
`

// CopySlackSettings copies the settings an existing Slack app needs, as a
// manifest snippet, to the clipboard.
func (a *AppService) CopySlackSettings() error {
	if !a.app.Clipboard.SetText(slackSettingsSnippet) {
		return errors.New("could not copy to the clipboard")
	}
	return nil
}

// OpenSlackAppPage opens api.slack.com's "create an app" page with the
// manifest filled in.
func (a *AppService) OpenSlackAppPage() error {
	return a.app.Browser.OpenURL("https://api.slack.com/apps?new_app=1&manifest_yaml=" + url.QueryEscape(assets.SlackAppManifest))
}

// ---- app preferences -----------------------------------------------------

// Prefs are GUI preferences.
type Prefs struct {
	Notifications bool `json:"notifications"`
	// Language is "auto" (follow the system), "en" or "ja". It is used for
	// the app and for the messages posted to Slack.
	Language string `json:"language"`
	// MCPClient is the MCP client last chosen in settings (see mcp.go).
	MCPClient string `json:"mcp_client"`
}

func (a *AppService) prefsPath() string { return filepath.Join(a.dirs.DataDir, "gui.json") }

// GetPrefs reads GUI preferences.
func (a *AppService) GetPrefs() Prefs {
	p := Prefs{Notifications: true, Language: locale.Auto}
	if b, err := os.ReadFile(a.prefsPath()); err == nil {
		_ = json.Unmarshal(b, &p)
	}
	if !locale.Valid(p.Language) || p.Language == "" {
		p.Language = locale.Auto
	}
	if !slices.Contains(mcpClients, p.MCPClient) {
		p.MCPClient = mcpClients[0]
	}
	return p
}

// SetPrefs writes GUI preferences. A language change is applied right away,
// to the app and to the messages posted to Slack (settings.language).
func (a *AppService) SetPrefs(p Prefs) error {
	if !locale.Valid(p.Language) {
		return fmt.Errorf("unsupported language %q", p.Language)
	}
	old := a.GetPrefs()
	before := a.Locale()
	b, _ := json.MarshalIndent(p, "", "  ")
	if err := os.MkdirAll(a.dirs.DataDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(a.prefsPath(), b, 0o600); err != nil {
		return err
	}
	if p.Language != old.Language {
		if err := a.syncLanguage(p.Language); err != nil {
			return err
		}
	}
	if after := a.Locale(); after != before {
		a.app.Event.Emit(evLocale, after)
		if a.onLocale != nil {
			a.onLocale()
		}
	}
	return nil
}

// syncLanguage writes the language into the config so canned Slack
// messages use the same language as the app.
func (a *AppService) syncLanguage(lang string) error {
	data, _, err := a.readRaw()
	if err != nil {
		return err
	}
	content, err := config.SetSetting(data, "language", lang)
	if err != nil {
		return err
	}
	res, err := a.saveConfig(string(content))
	if err != nil {
		return err
	}
	if !res.OK {
		return errors.New(strings.Join(res.Problems, "\n"))
	}
	return nil
}

// Locale returns the language the UI should use ("en" or "ja").
func (a *AppService) Locale() string { return locale.Resolve(a.GetPrefs().Language) }

// LaunchAtLogin reports whether the app starts at login.
func (a *AppService) LaunchAtLogin() bool {
	on, _ := a.app.Autostart.IsEnabled()
	return on
}

// SetLaunchAtLogin turns starting the app at login on or off.
func (a *AppService) SetLaunchAtLogin(on bool) error {
	if on {
		return a.app.Autostart.Enable()
	}
	return a.app.Autostart.Disable()
}

// ---- windows and misc ----------------------------------------------------

// Version returns the app version.
func (a *AppService) Version() string { return a.version }

// OpenMain shows the window. view is "" (the rule list), a rule name or
// viewSettings.
func (a *AppService) OpenMain(view string) {
	if a.main == nil {
		return
	}
	a.app.Event.Emit(evNavigate, view)
	a.main.Show()
	a.main.Focus()
}

// viewSettings is the navigate target for the settings view. Rule names
// cannot start with ':', so it never names a rule.
const viewSettings = ":settings"

// OpenSettings shows the window on the settings view.
func (a *AppService) OpenSettings() { a.OpenMain(viewSettings) }

// OpenURL opens a link in the default browser.
func (a *AppService) OpenURL(link string) error {
	if !strings.HasPrefix(link, "https://") {
		return errors.New("only https links can be opened")
	}
	return a.app.Browser.OpenURL(link)
}

// ShowLogs opens the gateway log (in Console).
func (a *AppService) ShowLogs() error {
	if _, err := os.Stat(a.dirs.LogFile()); err != nil {
		return exec.Command("open", a.dirs.LogDir).Run()
	}
	return exec.Command("open", a.dirs.LogFile()).Run()
}
