package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// CheckClaude asks the gateway whether Claude Code is signed in (docs/ia.md
// §11-21). It returns at once; a changed answer reaches the UI in the
// snapshot. force skips the gateway's recent answer (after signing in).
func (a *AppService) CheckClaude(force bool) {
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = a.client.CheckClaude(c, force)
	}()
}

// SignInClaude opens Terminal and starts signing in to the Claude Code that
// rules run (settings.claude_command), which can differ from the `claude`
// the user's shell finds.
func (a *AppService) SignInClaude() error {
	_, cfg, err := a.readRaw()
	if err != nil {
		return err
	}
	script, err := claudeSignInScript(cfg.Settings.ClaudeCommand, cfg.SearchPath())
	if err != nil {
		return err
	}
	p := filepath.Join(a.dirs.DataDir, "claude-sign-in.command")
	if err := os.MkdirAll(a.dirs.DataDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(p, 0o700); err != nil {
		return err
	}
	return exec.Command("open", p).Run()
}

// SignInCodex opens Terminal and starts signing in to the Codex that rules
// run (settings.codex_command) with `codex login`.
func (a *AppService) SignInCodex() error {
	_, cfg, err := a.readRaw()
	if err != nil {
		return err
	}
	cmd := []string(cfg.Settings.CodexCommand)
	if len(cmd) == 0 {
		cmd = []string{"codex"}
	}
	script, err := signInScript(cmd, cfg.SearchPath(), "login")
	if err != nil {
		return err
	}
	return a.openScript("codex-sign-in.command", script)
}

// openScript writes a Terminal script to the data folder and opens it.
func (a *AppService) openScript(name, script string) error {
	p := filepath.Join(a.dirs.DataDir, name)
	if err := os.MkdirAll(a.dirs.DataDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(p, 0o700); err != nil {
		return err
	}
	return exec.Command("open", p).Run()
}

// signInScript is a Terminal script that runs `<command> <args…>`.
func signInScript(command []string, pathEnv string, args ...string) (string, error) {
	if len(command) == 0 {
		return "", errors.New("no command configured")
	}
	words := make([]string, 0, len(command)+len(args))
	for _, w := range command {
		words = append(words, shellQuote(w))
	}
	words = append(words, args...)
	return "#!/bin/sh\nexport PATH=" + shellQuote(pathEnv) + "\nexec " + strings.Join(words, " ") + "\n", nil
}

// claudeSignInScript is a Terminal script that runs `<command> auth login`.
func claudeSignInScript(command []string, pathEnv string) (string, error) {
	if len(command) == 0 {
		return "", errors.New("no claude command configured")
	}
	words := make([]string, 0, len(command)+2)
	for _, w := range command {
		words = append(words, shellQuote(w))
	}
	words = append(words, "auth", "login")
	return "#!/bin/sh\nexport PATH=" + shellQuote(pathEnv) + "\nexec " + strings.Join(words, " ") + "\n", nil
}
