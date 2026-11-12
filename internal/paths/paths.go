// Package paths resolves where sockdock keeps its config, state and logs.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "sockdock"

// Dirs holds the resolved locations.
type Dirs struct {
	ConfigFile string // config.yaml
	DataDir    string // database, lock file, control socket
	LogDir     string
}

// Default resolves directories for the current platform.
//
// Overrides:
//   - SOCKDOCK_CONFIG: path to config.yaml
//   - SOCKDOCK_DATA_DIR: data directory (logs go to <data>/logs unless SOCKDOCK_LOG_DIR is set)
//   - SOCKDOCK_LOG_DIR: log directory
func Default() Dirs {
	home, _ := os.UserHomeDir()
	var d Dirs
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support", appName)
		d = Dirs{
			ConfigFile: filepath.Join(base, "config.yaml"),
			DataDir:    base,
			LogDir:     filepath.Join(home, "Library", "Logs", appName),
		}
	default:
		cfgHome := envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
		stateHome := envOr("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
		d = Dirs{
			ConfigFile: filepath.Join(cfgHome, appName, "config.yaml"),
			DataDir:    filepath.Join(stateHome, appName),
			LogDir:     filepath.Join(stateHome, appName, "logs"),
		}
	}
	if v := os.Getenv("SOCKDOCK_CONFIG"); v != "" {
		d.ConfigFile = v
	}
	if v := os.Getenv("SOCKDOCK_DATA_DIR"); v != "" {
		d.DataDir = v
		d.LogDir = filepath.Join(v, "logs")
	}
	if v := os.Getenv("SOCKDOCK_LOG_DIR"); v != "" {
		d.LogDir = v
	}
	return d
}

// DBPath is the SQLite database file.
func (d Dirs) DBPath() string { return filepath.Join(d.DataDir, "sockdock.db") }

// SocketPath is the control API socket.
func (d Dirs) SocketPath() string { return filepath.Join(d.DataDir, "control.sock") }

// LockPath is the single-instance lock file.
func (d Dirs) LockPath() string { return filepath.Join(d.DataDir, "sockdock.lock") }

// LogFile is the main log file.
func (d Dirs) LogFile() string { return filepath.Join(d.LogDir, "sockdock.log") }

// PromptDir holds prompt files written at startup.
func (d Dirs) PromptDir() string { return filepath.Join(d.DataDir, "prompts") }

// Ensure creates the data and log directories.
func (d Dirs) Ensure() error {
	for _, p := range []string{d.DataDir, d.LogDir, filepath.Dir(d.ConfigFile)} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// ExpandHome replaces a leading "~" with the user's home directory.
func ExpandHome(p string) string {
	if p == "~" || len(p) > 1 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[1:])
	}
	return p
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
