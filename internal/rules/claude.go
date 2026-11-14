package rules

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

var (
	shellPathOnce sync.Once
	shellPath     string
)

// LoginShellPath returns the PATH of the user's interactive login shell, so
// SockDock finds the same tools (claude, uv, ...) as their terminal. Apps
// launched from Finder only get a minimal PATH.
func LoginShellPath() string {
	shellPathOnce.Do(func() {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/zsh"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, shell, "-l", "-i", "-c", `printf '__SOCKDOCK_PATH__%s' "$PATH"`).Output()
		if err == nil {
			if _, p, ok := strings.Cut(string(out), "__SOCKDOCK_PATH__"); ok && p != "" {
				shellPath = strings.TrimSpace(p)
				return
			}
		}
		shellPath = os.Getenv("PATH")
		if shellPath == "" {
			shellPath = "/usr/bin:/bin:/usr/sbin:/sbin"
		}
	})
	return shellPath
}

// DetectClaude lists Claude Code executables that run without a shell shim.
func DetectClaude() []string {
	home, _ := os.UserHomeDir()
	cands := []string{
		filepath.Join(home, ".local", "bin", "claude"),
		filepath.Join(home, ".claude", "local", "claude"),
		filepath.Join(home, ".local", "share", "mise", "installs", "claude", "latest", "claude"),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	}
	if p, err := config.LookPath("claude", LoginShellPath()); err == nil {
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

// DetectCodex lists Codex executables that run without a shell shim.
func DetectCodex() []string {
	home, _ := os.UserHomeDir()
	cands := []string{
		filepath.Join(home, ".local", "share", "mise", "installs", "codex", "latest", "bin", "codex"),
		filepath.Join(home, ".local", "bin", "codex"),
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
	}
	if p, err := config.LookPath("codex", LoginShellPath()); err == nil {
		cands = append([]string{p}, cands...)
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range cands {
		if seen[c] || !isExecutable(c) || strings.Contains(c, "/shims/") {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}
