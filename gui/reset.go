package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
	"github.com/iwamatsu0430/sock-dock-slack/internal/secret"
)

// ResetAll returns SockDock to its first-launch state:
// it stops runs and the gateway, removes the Keychain
// tokens, moves the config file to the Trash and writes a minimal one,
// deletes the run history, resets the app's preferences and turns off
// starting at login. Logs are kept. The window then shows setup.
func (a *AppService) ResetAll() error {
	if a.emb.State().State == GatewayExternal {
		return errors.New(a.tr("err.resetService"))
	}
	if externalTokens(a.secretsFile()) {
		return errors.New(a.tr("err.resetEnv"))
	}

	a.stopRuns(10 * time.Second)
	a.emb.Stop(30 * time.Second)
	defer a.emb.Start() // reports "setup" without tokens

	var errs []error
	for _, name := range []string{secret.AppToken, secret.BotToken} {
		_ = secret.KeychainDelete(name) // a missing item is fine
	}
	if err := a.trashConfig(); err != nil {
		errs = append(errs, err)
	}
	for _, p := range []string{a.dirs.DBPath(), a.dirs.DBPath() + "-wal", a.dirs.DBPath() + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}

	before := a.Locale()
	if err := os.Remove(a.prefsPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if a.LaunchAtLogin() {
		if err := a.SetLaunchAtLogin(false); err != nil {
			errs = append(errs, err)
		}
	}
	a.chMu.Lock()
	clear(a.channels)
	a.chMu.Unlock()
	if after := a.Locale(); after != before {
		a.app.Event.Emit(evLocale, after)
		if a.onLocale != nil {
			a.onLocale()
		}
	}
	a.app.Event.Emit(evNavigate, "")
	return errors.Join(errs...)
}

// externalTokens reports tokens set in the environment or in secrets_file,
// which SockDock cannot remove.
func externalTokens(secretsFile string) bool {
	for _, name := range []string{secret.AppToken, secret.BotToken} {
		if os.Getenv(name) != "" {
			return true
		}
	}
	if secretsFile == "" {
		return false
	}
	vals, err := config.ReadDotenv(secretsFile)
	return err == nil && (vals[secret.AppToken] != "" || vals[secret.BotToken] != "")
}

// stopRuns stops the runs in progress and waits (up to timeout) for them to
// finish, so each thread gets the "stopped" message.
func (a *AppService) stopRuns(timeout time.Duration) {
	snap := a.GetSnapshot()
	if snap.Status == nil || len(snap.Status.Running) == 0 {
		return
	}
	c, cancel := ctx()
	for _, r := range snap.Status.Running {
		_ = a.client.StopTurn(c, r.TurnID)
	}
	cancel()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s := a.GetSnapshot(); s.Status == nil || len(s.Status.Running) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// trashConfig moves the config file to the Trash (or, without the trash
// command, renames it next to itself) and writes a minimal config.
func (a *AppService) trashConfig() error {
	path := a.dirs.ConfigFile
	if _, err := os.Stat(path); err == nil {
		if out, err := exec.Command("/usr/bin/trash", path).CombinedOutput(); err != nil {
			bak := fmt.Sprintf("%s.%s.bak", path, time.Now().Format("20060102-150405"))
			if rerr := os.Rename(path, bak); rerr != nil {
				return fmt.Errorf("move the config file aside: %v (%s)", rerr, out)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(config.MinimalConfig), 0o600)
}
