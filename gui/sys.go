package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// findSockdock locates the CLI binary the background service should run:
// next to the GUI executable (inside the .app), then PATH, then Homebrew.
func findSockdock() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if p, err := filepath.EvalSymlinks(exe); err == nil {
			exe = p
		}
		cand := filepath.Join(filepath.Dir(exe), "sockdock")
		if isExecutable(cand) {
			return cand, nil
		}
	}
	pathEnv := loginShellPath() + string(os.PathListSeparator) + "/opt/homebrew/bin:/usr/local/bin"
	if p, err := config.LookPath("sockdock", pathEnv); err == nil {
		return p, nil
	}
	return "", errors.New("could not find the sockdock command; install it with Homebrew (brew install sock-dock-slack)")
}

var cachedPath string

// loginShellPath returns the PATH of the user's interactive login shell, so
// the service finds the same tools (claude, uv, ...) as their terminal.
// Apps launched from Finder only get a minimal PATH.
func loginShellPath() string {
	if cachedPath != "" {
		return cachedPath
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-l", "-i", "-c", `printf '__SOCKDOCK_PATH__%s' "$PATH"`).Output()
	if err == nil {
		if _, p, ok := strings.Cut(string(out), "__SOCKDOCK_PATH__"); ok && p != "" {
			cachedPath = strings.TrimSpace(p)
			return cachedPath
		}
	}
	cachedPath = os.Getenv("PATH")
	if cachedPath == "" {
		cachedPath = "/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return cachedPath
}

// detectClaude lists Claude Code executables that run without a shell shim.
func detectClaude() []string {
	home, _ := os.UserHomeDir()
	cands := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		filepath.Join(home, ".local", "share", "mise", "installs", "claude", "latest", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	}
	if p, err := config.LookPath("claude", loginShellPath()); err == nil {
		cands = append([]string{p}, cands...)
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if seen[c] || !isExecutable(c) {
			continue
		}
		seen[c] = true
		// Version managers' shims (mise, asdf) may fail outside a shell; skip them.
		if strings.Contains(c, "/shims/") {
			continue
		}
		out = append(out, c)
	}
	return out
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// notify shows a macOS notification. osascript works without code signing.
func notify(title, msg string) {
	esc := func(s string) string {
		return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
	}
	_ = exec.Command("osascript", "-e", `display notification "`+esc(msg)+`" with title "`+esc(title)+`"`).Start()
}
