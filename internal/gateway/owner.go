package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
	"github.com/iwamatsu0430/sock-dock-slack/internal/handler"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// The owner ("本人") is the person who uses SockDock. They are registered by
// sending, by DM to the Slack app, a one-time code (TOTP) shown on the Mac;
// notices then go to them by DM (docs/design.md §3.5).

const (
	stateKeyOwnerDM        = "owner_dm"
	stateKeyClaudeNotified = "claude_signed_out_notified"
	stateKeyCodexNotified  = "codex_signed_out_notified"

	registrationTTL = 10 * time.Minute
	maxWrongCodes   = 5
	totpStep        = 30 * time.Second
	totpDigits      = 6
)

// registration is an owner registration in progress. The secret lives only
// in memory and is dropped when the registration ends.
type registration struct {
	secret  []byte
	started time.Time
	wrong   int
}

// ownerDM is the owner's DM channel with the Slack app.
type ownerDM struct {
	User    string `json:"user"`
	Channel string `json:"channel"`
}

// Registration is what the app shows while registering the owner.
type Registration struct {
	// Active is true while codes are accepted.
	Active bool `json:"active"`
	// Code is the current code; NextChange is when it changes.
	Code       string     `json:"code,omitempty"`
	NextChange *time.Time `json:"next_change,omitempty"`
	StepSec    int        `json:"step_sec,omitempty"`
	// Ended is how the last registration ended: "registered", "expired",
	// "wrong_codes" or "cancelled". The app does not show it; the CLI does.
	Ended string `json:"ended,omitempty"`
}

// StartRegistration starts accepting owner codes by DM (starting again makes
// a new secret).
func (g *Gateway) StartRegistration() (Registration, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return Registration{}, err
	}
	g.mu.Lock()
	g.reg = &registration{secret: secret, started: g.opts.Now()}
	g.regEnded = ""
	g.mu.Unlock()
	g.broadcast(Event{Type: "owner"})
	return g.Registration(), nil
}

// CancelRegistration stops accepting owner codes.
func (g *Gateway) CancelRegistration() {
	g.mu.Lock()
	had := g.reg != nil
	if had {
		g.reg, g.regEnded = nil, "cancelled"
	}
	g.mu.Unlock()
	if had {
		g.broadcast(Event{Type: "owner"})
	}
}

// Registration returns the registration in progress, if any.
func (g *Gateway) Registration() Registration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.opts.Now()
	if !g.regActiveLocked(now) {
		ended := g.regEnded
		if g.reg != nil {
			ended = "expired"
		}
		return Registration{Ended: ended}
	}
	step := now.Truncate(totpStep)
	next := step.Add(totpStep)
	return Registration{Active: true, Code: totp(g.reg.secret, now), NextChange: &next, StepSec: int(totpStep / time.Second)}
}

func (g *Gateway) regActiveLocked(now time.Time) bool {
	return g.reg != nil && now.Sub(g.reg.started) < registrationTTL
}

// expireRegistration ends a registration that ran past its time limit. The
// app is told, but nothing is shown: it goes back to "not registered".
func (g *Gateway) expireRegistration() {
	g.mu.Lock()
	expired := g.reg != nil && !g.regActiveLocked(g.opts.Now())
	if expired {
		g.reg, g.regEnded = nil, "expired"
	}
	g.mu.Unlock()
	if expired {
		g.broadcast(Event{Type: "owner"})
	}
}

// handleCode treats a top-level DM made only of digits as an owner code while
// a registration is in progress. It reports whether the message was used.
func (g *Gateway) handleCode(ctx context.Context, m event.Message) bool {
	code, ok := normalizeCode(m.Text)
	if !ok {
		return false
	}
	g.mu.Lock()
	now := g.opts.Now()
	if !g.regActiveLocked(now) {
		g.mu.Unlock()
		return false
	}
	secret := g.reg.secret
	match := hmac.Equal([]byte(code), []byte(totp(secret, now))) || hmac.Equal([]byte(code), []byte(totp(secret, now.Add(-totpStep))))
	ended := false
	if match {
		g.reg, g.regEnded = nil, "registered"
	} else {
		g.reg.wrong++
		if g.reg.wrong >= maxWrongCodes {
			g.reg, g.regEnded, ended = nil, "wrong_codes", true
		}
	}
	g.mu.Unlock()

	msgs := g.cfg.Load().Settings.Messages
	if !match {
		g.log.Info("wrong owner code", "user", m.User)
		_ = g.opts.API.PostMessage(ctx, m.Channel, "", msgs.WrongCode)
		if ended {
			g.broadcast(Event{Type: "owner"})
		}
		return true
	}
	if err := g.setOwner(ctx, m.User, m.Channel); err != nil {
		g.log.Error("could not register the owner", "err", err)
		return true
	}
	g.log.Info("owner registered", "user", m.User)
	_ = g.opts.API.PostMessage(ctx, m.Channel, "", g.cfg.Load().Settings.Messages.Registered)
	g.broadcast(Event{Type: "owner"})
	return true
}

// setOwner writes the owner to the config, turns on DM notices and keeps the
// DM channel.
func (g *Gateway) setOwner(ctx context.Context, user, channel string) error {
	err := g.editSettings(func(data []byte) ([]byte, error) {
		out, err := config.SetSettingValue(data, "owner", user)
		if err != nil {
			return nil, err
		}
		return config.SetSettingValue(out, "notify_dm", true)
	})
	if err != nil {
		return err
	}
	g.rememberOwnerDM(ctx, user, channel)
	return nil
}

// ClearOwner removes the owner and turns off DM notices.
func (g *Gateway) ClearOwner() error {
	err := g.editSettings(func(data []byte) ([]byte, error) {
		return config.SetSettingValue(config.DeleteSetting(data, "owner"), "notify_dm", false)
	})
	if err != nil {
		return err
	}
	g.broadcast(Event{Type: "owner"})
	return nil
}

// SetSettingBool turns DM notices ("notify_dm"), DM notices of failed runs
// ("notify_dm_failures") or the check for a newer version ("check_updates")
// on or off.
func (g *Gateway) SetSettingBool(key string, on bool) error {
	if key != "notify_dm" && key != "notify_dm_failures" && key != "check_updates" {
		return fmt.Errorf("unknown setting %q", key)
	}
	if err := g.editSettings(func(data []byte) ([]byte, error) { return config.SetSettingValue(data, key, on) }); err != nil {
		return err
	}
	g.broadcast(Event{Type: "owner"})
	return nil
}

// rememberOwnerDM keeps the owner's DM channel, so notices reach it.
func (g *Gateway) rememberOwnerDM(ctx context.Context, user, channel string) {
	dm := ownerDM{User: user, Channel: channel}
	g.mu.Lock()
	same := g.ownerDM == dm
	g.ownerDM = dm
	g.mu.Unlock()
	if same {
		return
	}
	if err := g.opts.Store.SetState(ctx, stateKeyOwnerDM, dm); err != nil {
		g.log.Warn("could not keep the owner's DM channel", "err", err)
	}
}

// editSettings changes the config file, checks it and reloads it.
func (g *Gateway) editSettings(f func([]byte) ([]byte, error)) error {
	g.cfgWrite.Lock()
	defer g.cfgWrite.Unlock()
	path := g.opts.ConfigPath
	if path == "" {
		return errors.New("no config file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := f(data)
	if err != nil {
		return err
	}
	if _, err := config.ParseLenient(out, path); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(out)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o600), os.Rename(tmp.Name(), path)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return g.Reload()
}

// notifyOwner sends a notice to the owner by DM when DM notices are on.
func (g *Gateway) notifyOwner(ctx context.Context, text string) {
	s := g.cfg.Load().Settings
	if !s.NotifyDM || s.Owner == "" {
		return
	}
	ch := s.Owner
	g.mu.Lock()
	if g.ownerDM.User == s.Owner && g.ownerDM.Channel != "" {
		ch = g.ownerDM.Channel
	}
	g.mu.Unlock()
	// Without a known DM channel, a member ID as the channel posts to the
	// app's DM with them (to be confirmed with a real Slack: docs/design.md §3.5).
	if err := g.opts.API.PostMessage(ctx, ch, "", text); err != nil {
		g.log.Warn("could not send a notice to the owner", "err", err)
	}
}

// NotifyRuleChanged tells the owner by DM that a rule was changed through MCP.
func (g *Gateway) NotifyRuleChanged(channel, kind, pattern string) {
	cfg := g.cfg.Load()
	label := ruleLabel(cfg.Settings.ResolvedLanguage, channel, kind, pattern)
	g.notifyOwner(context.WithoutCancel(g.runCtx), strings.ReplaceAll(cfg.Settings.Messages.RuleChangedMCP, "{rule}", label))
}

// notifyRunFailed tells the owner by DM that a run failed, when they asked
// for it (notify_dm_failures).
func (g *Gateway) notifyRunFailed(ctx context.Context, cfg *config.Config, r *config.Route, status string, runErr error, permalink string) {
	if !cfg.Settings.NotifyDMFailures {
		return
	}
	lang := cfg.Settings.ResolvedLanguage
	t := dmText[lang]
	if t == nil {
		t = dmText["en"]
	}
	reason := t["reason.error"]
	var ee *handler.ExitError
	switch {
	case status == store.StatusTimeout:
		reason = strings.ReplaceAll(t["reason.timeout"], "{timeout_sec}", fmt.Sprint(r.Handler.TimeoutSec))
	case errors.As(runErr, &ee) && ee.Reason == handler.ReasonSignedOut:
		reason = t["reason.signed_out"]
	case errors.As(runErr, &ee) && ee.Reason == handler.ReasonCodexSignedOut:
		reason = t["reason.codex_signed_out"]
	}
	link := ""
	if permalink != "" {
		link = "<" + permalink + "|" + t["thread"] + ">"
	}
	text := strings.NewReplacer("{rule}", ruleLabel(lang, r.Channel, r.Kind(), r.Pattern()), "{reason}", reason, "{link}", link).
		Replace(cfg.Settings.Messages.RunFailed)
	g.notifyOwner(ctx, strings.TrimSpace(text))
}

// noticeSignedOut tells the owner by DM once that an agent is signed out;
// clearSignedOutNotice (on signing in) lets it be told again.
func (g *Gateway) noticeSignedOut(agent string) {
	g.claudeNotice.Lock()
	defer g.claudeNotice.Unlock()
	g.mu.Lock()
	v := g.signedInLocked(agent)
	g.mu.Unlock()
	cfg := g.cfg.Load()
	if v == nil || *v || !cfg.Settings.NotifyDM || cfg.Settings.Owner == "" || !hasAgentRoutes(cfg, agent) {
		return
	}
	key, text := stateKeyClaudeNotified, cfg.Settings.Messages.ClaudeSignedOut
	if agent == config.TypeCodex {
		key, text = stateKeyCodexNotified, cfg.Settings.Messages.CodexSignedOut
	}
	ctx := context.WithoutCancel(g.runCtx)
	var notified bool
	if _, err := g.opts.Store.GetState(ctx, key, &notified); err != nil || notified {
		return
	}
	g.notifyOwner(ctx, text)
	_ = g.opts.Store.SetState(ctx, key, true)
}

func (g *Gateway) clearSignedOutNotice(agent string) {
	g.claudeNotice.Lock()
	defer g.claudeNotice.Unlock()
	key := stateKeyClaudeNotified
	if agent == config.TypeCodex {
		key = stateKeyCodexNotified
	}
	ctx := context.WithoutCancel(g.runCtx)
	var notified bool
	if _, err := g.opts.Store.GetState(ctx, key, &notified); err == nil && notified {
		_ = g.opts.Store.SetState(ctx, key, false)
	}
}

// dmText holds the parts of DM notices that are not configurable.
var dmText = map[string]map[string]string{
	"en": {
		"keyword":                 "%s “%s”",
		"regex":                   "%s /%s/",
		"dm":                      "DM",
		"thread":                  "Thread",
		"release_notes":           "Release notes",
		"reason.timeout":          "It ran past the time limit ({timeout_sec} seconds) and was stopped.",
		"reason.signed_out":       "Claude Code isn't signed in.",
		"reason.codex_signed_out": "Codex isn't signed in.",
		"reason.error":            "The reason is shown in SockDock on the Mac.",
	},
	"ja": {
		"keyword":                 "%s「%s」",
		"regex":                   "%s /%s/",
		"dm":                      "DM",
		"thread":                  "スレッド",
		"release_notes":           "リリースノート",
		"reason.timeout":          "上限時間（{timeout_sec} 秒）を超えたため停止しました。",
		"reason.signed_out":       "Claude Code にサインインしていません。",
		"reason.codex_signed_out": "Codex にサインインしていません。",
		"reason.error":            "理由は Mac の SockDock で確認できます。",
	},
}

// ruleLabel names a rule the way menus and notifications do: #channel「keyword」,
// #channel /regex/, #channel, or DM「keyword」. Channels are Slack channel
// links, which Slack shows by name.
func ruleLabel(lang, channel, kind, pattern string) string {
	t := dmText[lang]
	if t == nil {
		t = dmText["en"]
	}
	ch := "<#" + channel + ">"
	if channel == config.ChannelDM {
		ch = t["dm"]
	}
	switch kind {
	case config.KindStartsWith, config.KindContains:
		return fmt.Sprintf(t["keyword"], ch, pattern)
	case config.KindMatch:
		return fmt.Sprintf(t["regex"], ch, pattern)
	}
	return ch
}

// totp is the RFC 6238 code (HMAC-SHA1, 30-second steps, 6 digits) at t.
func totp(secret []byte, t time.Time) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(t.Unix()/int64(totpStep/time.Second)))
	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000)
}

// normalizeCode turns a DM into a code: spaces are dropped and full-width
// digits become ASCII. It reports false unless exactly six digits remain.
func normalizeCode(text string) (string, bool) {
	var b strings.Builder
	for _, r := range strings.TrimSpace(text) {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= '０' && r <= '９':
			b.WriteRune('0' + (r - '０'))
		case r == ' ' || r == '　' || r == '-':
		default:
			return "", false
		}
	}
	if b.Len() != totpDigits {
		return "", false
	}
	return b.String(), true
}
