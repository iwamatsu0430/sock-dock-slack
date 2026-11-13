// Package secret finds the Slack tokens: environment, macOS Keychain, or a
// secrets file.
package secret

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/iwamatsu0430/sock-dock-slack/internal/config"
)

// Token names.
const (
	AppToken = "SLACK_APP_TOKEN"
	BotToken = "SLACK_BOT_TOKEN"
)

// KeychainService is the Keychain service name sockdock uses.
// SOCKDOCK_KEYCHAIN_SERVICE overrides it (for development, so a test copy
// does not touch the real tokens).
var KeychainService = keychainService()

func keychainService() string {
	if v := os.Getenv("SOCKDOCK_KEYCHAIN_SERVICE"); v != "" {
		return v
	}
	return "sockdock"
}

// Source says where a token was found.
type Source string

const (
	SourceEnv      Source = "environment"
	SourceKeychain Source = "keychain"
	SourceFile     Source = "secrets_file"
	SourceNone     Source = ""
)

// Lookup returns a token and where it came from. secretsFile may be empty.
func Lookup(name, secretsFile string) (string, Source, error) {
	if v := os.Getenv(name); v != "" {
		return v, SourceEnv, nil
	}
	if runtime.GOOS == "darwin" {
		if v, err := keychainGet(name); err == nil && v != "" {
			return v, SourceKeychain, nil
		}
	}
	if secretsFile != "" {
		if err := checkPerm(secretsFile); err != nil {
			return "", SourceNone, err
		}
		vals, err := config.ReadDotenv(secretsFile)
		if err != nil {
			return "", SourceNone, err
		}
		if v := vals[name]; v != "" {
			return v, SourceFile, nil
		}
	}
	return "", SourceNone, nil
}

// Tokens returns both tokens or an error naming what is missing.
func Tokens(secretsFile string) (app, bot string, err error) {
	app, _, err = Lookup(AppToken, secretsFile)
	if err != nil {
		return "", "", err
	}
	bot, _, err = Lookup(BotToken, secretsFile)
	if err != nil {
		return "", "", err
	}
	var missing []string
	if app == "" {
		missing = append(missing, AppToken)
	} else if !strings.HasPrefix(app, "xapp-") {
		return "", "", fmt.Errorf("%s should start with xapp-", AppToken)
	}
	if bot == "" {
		missing = append(missing, BotToken)
	} else if !strings.HasPrefix(bot, "xoxb-") {
		return "", "", fmt.Errorf("%s should start with xoxb-", BotToken)
	}
	if len(missing) > 0 {
		return "", "", fmt.Errorf("missing %s: run `sockdock auth set`, export them, or set settings.secrets_file", strings.Join(missing, " and "))
	}
	return app, bot, nil
}

func keychainGet(name string) (string, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", KeychainService, "-a", name, "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// KeychainSetInteractive stores a token in the Keychain, letting `security`
// prompt for the value on the terminal so it never appears in argv.
func KeychainSetInteractive(name string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("the Keychain is only available on macOS; use environment variables or settings.secrets_file")
	}
	cmd := exec.Command("/usr/bin/security", "add-generic-password", "-U", "-s", KeychainService, "-a", name,
		"-l", "sockdock "+name, "-w")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// KeychainSet stores a token in the Keychain non-interactively (used by the
// GUI). The value is passed to /usr/bin/security as an argument, so it is
// briefly visible to other processes of the same user via ps.
func KeychainSet(name, value string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("the Keychain is only available on macOS")
	}
	out, err := exec.Command("/usr/bin/security", "add-generic-password", "-U", "-s", KeychainService, "-a", name,
		"-l", "sockdock "+name, "-w", value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("security: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// KeychainDelete removes a token from the Keychain.
func KeychainDelete(name string) error {
	return exec.Command("/usr/bin/security", "delete-generic-password", "-s", KeychainService, "-a", name).Run()
}

func checkPerm(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by others; run: chmod 600 %q", path, path)
	}
	return nil
}
