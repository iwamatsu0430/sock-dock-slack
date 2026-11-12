// Package locale picks the language for user-facing text (the GUI and the
// messages the gateway posts to Slack). The CLI and logs stay in English.
package locale

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Supported languages. The first one is the fallback.
var Supported = []string{"en", "ja"}

// Auto means "follow the system language".
const Auto = "auto"

// Valid reports whether v is "", "auto" or a supported language.
func Valid(v string) bool {
	return v == "" || v == Auto || Normalize(v) == v
}

// Normalize maps a tag like "ja-JP", "ja_JP.UTF-8" or "en" to a supported
// language, or returns "" if none matches.
func Normalize(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if i := strings.IndexAny(tag, "-_.@"); i >= 0 {
		tag = tag[:i]
	}
	for _, s := range Supported {
		if tag == s {
			return s
		}
	}
	return ""
}

// Resolve turns a preference ("", "auto" or a language) into a supported language.
func Resolve(pref string) string {
	if l := Normalize(pref); l != "" {
		return l
	}
	return System()
}

var (
	systemOnce sync.Once
	systemLang string
)

// System detects the user's language from LC_ALL / LC_MESSAGES / LANG and,
// on macOS, the AppleLanguages preference (services started by launchd
// usually have no LANG).
func System() string {
	systemOnce.Do(func() { systemLang = detect(os.Getenv, appleLanguages) })
	return systemLang
}

func detect(getenv func(string) string, apple func() []string) string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := getenv(k)
		if v == "" || v == "C" || v == "POSIX" || strings.HasPrefix(v, "C.") {
			continue
		}
		if l := Normalize(v); l != "" {
			return l
		}
		// A set but unsupported locale wins over the OS preference.
		return Supported[0]
	}
	for _, tag := range apple() {
		if l := Normalize(tag); l != "" {
			return l
		}
	}
	return Supported[0]
}

func appleLanguages() []string {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, err := exec.Command("/usr/bin/defaults", "read", "-g", "AppleLanguages").Output()
	if err != nil {
		return nil
	}
	// Output looks like: (\n    "ja-JP",\n    "en-US"\n)
	var tags []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.Trim(strings.TrimSpace(line), `",()`)
		if line != "" {
			tags = append(tags, line)
		}
	}
	return tags
}
