// Package service installs sockdock as a per-user background service:
// a launchd LaunchAgent on macOS, a systemd user unit on Linux.
package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"text/template"
)

// Label is the launchd label / systemd unit name.
const (
	Label    = "io.github.iwamatsu0430.sockdock"
	UnitName = "sockdock.service"
)

// Spec describes what the service runs.
type Spec struct {
	Binary     string // absolute path to sockdock
	ConfigFile string
	LogDir     string
	// Path is the PATH the service runs with (so handlers find claude, uv, ...).
	Path string
}

// DefaultBinary returns a stable path to the running executable. Homebrew
// Cellar paths (which change on upgrade) are mapped to the opt/ symlink.
func DefaultBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	return homebrewOpt(exe), nil
}

// StablePath maps a Homebrew Cellar path to its opt/ symlink when possible.
func StablePath(p string) string { return homebrewOpt(p) }

// Restart restarts a loaded service.
func Restart() error {
	switch runtime.GOOS {
	case "darwin":
		if out, err := exec.Command("launchctl", "kickstart", "-k", guiDomain()+"/"+Label).CombinedOutput(); err != nil {
			return fmt.Errorf("launchctl kickstart: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	case "linux":
		return exec.Command("systemctl", "--user", "restart", UnitName).Run()
	}
	return fmt.Errorf("services are not supported on %s", runtime.GOOS)
}

var cellarRe = regexp.MustCompile(`^(.*)/Cellar/([^/]+)/[^/]+/(.*)$`)

func homebrewOpt(p string) string {
	m := cellarRe.FindStringSubmatch(p)
	if m == nil {
		return p
	}
	opt := filepath.Join(m[1], "opt", m[2], m[3])
	if _, err := os.Stat(opt); err == nil {
		return opt
	}
	return p
}

// Install writes and loads the service definition.
func Install(s Spec) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(s)
	case "linux":
		return installSystemd(s)
	}
	return "", fmt.Errorf("services are not supported on %s", runtime.GOOS)
}

// Uninstall stops and removes the service definition.
func Uninstall() error {
	switch runtime.GOOS {
	case "darwin":
		p := plistPath()
		_ = exec.Command("launchctl", "bootout", guiDomain(), p).Run()
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	case "linux":
		_ = exec.Command("systemctl", "--user", "disable", "--now", UnitName).Run()
		if err := os.Remove(unitPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	return fmt.Errorf("services are not supported on %s", runtime.GOOS)
}

// Status returns a human-readable service status and whether it is loaded.
func Status() (string, bool) {
	switch runtime.GOOS {
	case "darwin":
		if _, err := os.Stat(plistPath()); err != nil {
			return "not installed", false
		}
		out, err := exec.Command("launchctl", "print", guiDomain()+"/"+Label).CombinedOutput()
		if err != nil {
			return "installed but not loaded (" + plistPath() + ")", false
		}
		state := "loaded"
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "state = ") || strings.HasPrefix(line, "pid = ") {
				state += ", " + line
			}
		}
		return state, true
	case "linux":
		out, err := exec.Command("systemctl", "--user", "is-active", UnitName).Output()
		return strings.TrimSpace(string(out)), err == nil
	}
	return "unsupported", false
}

func plistPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
}

func guiDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

var plistTmpl = template.Must(template.New("plist").Funcs(template.FuncMap{"xml": xmlEscape}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>{{.Label}}</string>
  <key>ProgramArguments</key>
  <array>
    <string>{{xml .Binary}}</string>
    <string>run</string>
    <string>--config</string>
    <string>{{xml .ConfigFile}}</string>
  </array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>{{xml .Path}}</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>{{xml .LogDir}}/launchd.out.log</string>
  <key>StandardErrorPath</key><string>{{xml .LogDir}}/launchd.err.log</string>
</dict>
</plist>
`))

func installLaunchd(s Spec) (string, error) {
	var buf bytes.Buffer
	if err := plistTmpl.Execute(&buf, struct {
		Spec
		Label string
	}{s, Label}); err != nil {
		return "", err
	}
	p := plistPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.LogDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	_ = exec.Command("launchctl", "bootout", guiDomain(), p).Run()
	if out, err := exec.Command("launchctl", "bootstrap", guiDomain(), p).CombinedOutput(); err != nil {
		return p, fmt.Errorf("launchctl bootstrap: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return p, nil
}

func unitPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", UnitName)
}

var unitTmpl = template.Must(template.New("unit").Parse(`[Unit]
Description=SockDock for Slack
After=network-online.target

[Service]
ExecStart="{{.Binary}}" run --config "{{.ConfigFile}}"
Environment="PATH={{.Path}}"
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`))

func installSystemd(s Spec) (string, error) {
	var buf bytes.Buffer
	if err := unitTmpl.Execute(&buf, s); err != nil {
		return "", err
	}
	p := unitPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "--now", UnitName}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return p, fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return p, nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
