package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/lockfile"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

func dirs(t *testing.T) paths.Dirs {
	t.Helper()
	dir := t.TempDir()
	return paths.Dirs{ConfigFile: filepath.Join(dir, "config.yaml"), DataDir: dir, LogDir: filepath.Join(dir, "logs")}
}

func TestStartWithoutConfig(t *testing.T) {
	d := dirs(t)
	_, err := Start(Options{Dirs: d})
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("err = %v, want ErrNoConfig", err)
	}
	// The lock must be released after a failed start.
	l, err := lockfile.Acquire(d.LockPath())
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	l.Release()
}

func TestStartWhileLocked(t *testing.T) {
	d := dirs(t)
	if err := os.MkdirAll(d.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.Acquire(d.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := Start(Options{Dirs: d}); !errors.Is(err, ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
}
