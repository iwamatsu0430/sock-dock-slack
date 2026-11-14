package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The cask that installs the app; `brew upgrade` updates it (docs/design.md §3.7).
const caskName = "iwamatsu0430/tap/sockdock"

// StartUpdate opens Terminal and updates SockDock with Homebrew. Homebrew
// quits the app while it upgrades, so the script opens it again afterwards.
func (a *AppService) StartUpdate() error {
	snap := a.GetSnapshot()
	if snap.Status == nil || snap.Status.Update == nil {
		return errors.New("no newer version is known")
	}
	if devBundle() {
		return errors.New("this is a local build; update it from its source")
	}
	script := updateScript(a.tr("update.start", snap.Status.Update.Version), a.tr("update.noBrew"), os.Getenv("PATH"))
	p := filepath.Join(a.dirs.DataDir, "update.command")
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

// updateScript is the Terminal script: say what happens, upgrade the cask,
// reopen the app on success. A failure leaves Homebrew's error on screen.
func updateScript(start, noBrew, pathEnv string) string {
	return "#!/bin/sh\n" +
		"export PATH=" + shellQuote(pathEnv) + "\n" +
		"echo " + shellQuote(start) + "\n" +
		"if ! command -v brew >/dev/null 2>&1; then echo " + shellQuote(noBrew) + "; exit 1; fi\n" +
		"brew upgrade --cask " + caskName + " && open -a SockDock\n"
}

// devBundle reports whether this app was built locally (its bundle ID ends in
// ".dev", scripts/build-app.sh); it is not the Homebrew cask, so it does not
// offer to update.
func devBundle() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	plist, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "..", "Info.plist"))
	if err != nil {
		return false
	}
	return strings.Contains(string(plist), ".sockdock.dev</string>")
}
