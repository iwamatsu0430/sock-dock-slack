// Package updates finds out whether a newer SockDock has been released
// (docs/design.md §3.7): it asks GitHub for the latest release, compares
// versions and picks the release notes in the app's language.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LatestURL is GitHub's latest published release of SockDock (drafts and
// prereleases are left out by GitHub).
const LatestURL = "https://api.github.com/repos/iwamatsu0430/sock-dock-slack/releases/latest"

// Release is a published release.
type Release struct {
	// Tag is the release's tag ("v0.2.0"); Version is it without the "v".
	Tag     string `json:"tag"`
	Version string `json:"version"`
	// URL is the release's page on GitHub.
	URL string `json:"url"`
	// Body is the release notes (Markdown).
	Body string `json:"body,omitempty"`
}

// Latest asks GitHub for the latest release.
func Latest(ctx context.Context, client *http.Client, url, userAgent string) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, err
	}
	if _, ok := Parse(body.TagName); !ok {
		return Release{}, errors.New("the latest release has no version tag: " + body.TagName)
	}
	return Release{Tag: body.TagName, Version: strings.TrimPrefix(body.TagName, "v"), URL: body.HTMLURL, Body: body.Body}, nil
}

var versionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// Parse reads a release version ("v1.2.3" or "1.2.3"). Builds that are not a
// release ("dev", "v0.1.1-6-g7e68b09", "0.0.0-…-dirty") do not parse.
func Parse(v string) ([3]int, bool) {
	m := versionRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// IsRelease reports whether this build is a release, the only kind that
// checks for updates.
func IsRelease(current string) bool {
	_, ok := Parse(current)
	return ok
}

// Newer reports whether latest is a newer release than current. It is false
// when either is not a release version.
func Newer(latest, current string) bool {
	l, ok1 := Parse(latest)
	c, ok2 := Parse(current)
	if !ok1 || !ok2 {
		return false
	}
	for i := range l {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// sectionTitles are the release notes' headings per language (the release
// skill writes both; see .claude/skills/release/SKILL.md).
var sectionTitles = map[string]string{"en": "English", "ja": "日本語"}

// Notes returns the release notes per language: the "## English" and
// "## 日本語" sections of body. A language whose section is missing gets the
// whole body, so notes written without sections still show.
func Notes(body string) map[string]string {
	out := map[string]string{}
	for lang := range sectionTitles {
		out[lang] = Section(body, lang)
	}
	return out
}

// Section returns the notes for one language (see Notes).
func Section(body, lang string) string {
	title, ok := sectionTitles[lang]
	body = strings.ReplaceAll(body, "\r\n", "\n")
	if !ok {
		return strings.TrimSpace(body)
	}
	var b strings.Builder
	in, found := false, false
	for _, line := range strings.Split(body, "\n") {
		if h, isHeading := strings.CutPrefix(line, "## "); isHeading {
			in = strings.TrimSpace(h) == title
			found = found || in
			continue
		}
		if in {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if !found {
		return strings.TrimSpace(body)
	}
	return strings.TrimSpace(b.String())
}
