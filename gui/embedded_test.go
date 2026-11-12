package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iwamatsu0430/sock-dock-slack/internal/lockfile"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

func testDirs(t *testing.T) paths.Dirs {
	t.Helper()
	dir := t.TempDir()
	return paths.Dirs{ConfigFile: filepath.Join(dir, "config.yaml"), DataDir: dir, LogDir: filepath.Join(dir, "logs")}
}

func TestEmbeddedNeedsConfig(t *testing.T) {
	changes := 0
	e := newEmbedded(testDirs(t), "test")
	e.onChange = func() { changes++ }
	st := e.Start()
	if st.State != GatewaySetup || st.Missing != "config" {
		t.Fatalf("state = %+v", st)
	}
	if changes == 0 {
		t.Error("onChange not called")
	}
	e.Stop(0)
	if got := e.State().State; got != GatewaySetup {
		t.Fatalf("stopping a gateway that never ran should keep the setup state, got %s", got)
	}
}

func TestEmbeddedDefersToAnotherSockdock(t *testing.T) {
	d := testDirs(t)
	if err := os.MkdirAll(d.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := lockfile.Acquire(d.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if st := newEmbedded(d, "test").Start(); st.State != GatewayExternal {
		t.Fatalf("state = %+v", st)
	}
}
