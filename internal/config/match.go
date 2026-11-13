package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Condition kinds: which top-level posts a route reacts to (docs/ia.md
// 「投稿の条件」). A route has exactly one.
const (
	KindStartsWith = "starts_with"
	KindContains   = "contains"
	KindMatch      = "match"
	KindAllPosts   = "all_posts"
)

// Kind returns the route's condition kind, or "" when none is set.
func (r *Route) Kind() string {
	switch {
	case strings.TrimSpace(r.StartsWith) != "":
		return KindStartsWith
	case strings.TrimSpace(r.Contains) != "":
		return KindContains
	case r.Match != "":
		return KindMatch
	case r.AllPosts:
		return KindAllPosts
	}
	return ""
}

// Pattern returns the keyword (starts_with, contains) or regex (match).
func (r *Route) Pattern() string {
	switch r.Kind() {
	case KindStartsWith:
		return strings.TrimSpace(r.StartsWith)
	case KindContains:
		return strings.TrimSpace(r.Contains)
	case KindMatch:
		return r.Match
	}
	return ""
}

// Condition describes the condition as written in the config ("starts_with: entry").
func (r *Route) Condition() string {
	switch k := r.Kind(); k {
	case "", KindAllPosts:
		return k
	default:
		return k + ": " + r.Pattern()
	}
}

// conditionProblem checks that exactly one condition is set and compiles a regex.
func (r *Route) conditionProblem() string {
	n := 0
	for _, set := range []bool{strings.TrimSpace(r.StartsWith) != "", strings.TrimSpace(r.Contains) != "", r.Match != "", r.AllPosts} {
		if set {
			n++
		}
	}
	switch {
	case n == 0:
		return "one of starts_with, contains, match or all_posts is required"
	case n > 1:
		return "use only one of starts_with, contains, match and all_posts"
	}
	if r.Match != "" {
		re, err := regexp.Compile(r.Match)
		if err != nil {
			return fmt.Sprintf("match: %v", err)
		}
		r.re = re
	}
	return ""
}

// upgradeLegacy turns a `match:` regex written by older versions of the app
// for a keyword into `starts_with:`.
func (r *Route) upgradeLegacy() {
	if r.StartsWith != "" || r.Contains != "" || r.AllPosts {
		return
	}
	if kw, ok := KeywordFromMatch(r.Match); ok {
		r.StartsWith, r.Match = kw, ""
	}
}

// MatchOptions tune Config.MatchWith.
type MatchOptions struct {
	// BotUserID is the Slack app's user ID; a leading mention of it is ignored.
	BotUserID string
	// Skip reports routes to pass over (turned off).
	Skip func(name string) bool
}

// Match finds the first route whose channel and condition match a top-level
// message. It returns the route and the values passed to handlers as groups
// ("text" for keyword and all-posts routes, named groups for regexes).
func (c *Config) Match(channel, text string) (*Route, map[string]string) {
	return c.MatchWith(channel, text, MatchOptions{})
}

// MatchWith is Match with options. Routes are tried in config order.
func (c *Config) MatchWith(channel, text string, o MatchOptions) (*Route, map[string]string) {
	body := postBody(text, o.BotUserID)
	for _, r := range c.Routes {
		if r.Channel != channel || (o.Skip != nil && o.Skip(r.Name)) {
			continue
		}
		if g, ok := r.matchBody(body); ok {
			return r, g
		}
	}
	return nil, nil
}

// matchBody matches a post body (see postBody).
func (r *Route) matchBody(body string) (map[string]string, bool) {
	switch r.Kind() {
	case KindStartsWith:
		rest, ok := startsWith(body, r.StartsWith)
		if !ok {
			return nil, false
		}
		return map[string]string{"text": rest}, true
	case KindContains:
		if !contains(body, r.Contains) {
			return nil, false
		}
		return map[string]string{"text": body}, true
	case KindAllPosts:
		return map[string]string{"text": body}, true
	case KindMatch:
		re := r.re
		if re == nil {
			var err error
			if re, err = regexp.Compile(r.Match); err != nil {
				return nil, false
			}
		}
		m := re.FindStringSubmatch(body)
		if m == nil {
			return nil, false
		}
		groups := map[string]string{}
		for i, name := range re.SubexpNames() {
			if i > 0 && name != "" {
				groups[name] = m[i]
			}
		}
		return groups, true
	}
	return nil, false
}

var slackEntities = strings.NewReplacer("&lt;", "<", "&gt;", ">", "&amp;", "&")

// postBody is the text conditions look at: Slack's &amp;/&lt;/&gt; turned
// back into characters, without a leading mention of the Slack app.
func postBody(text, botUserID string) string {
	s := strings.TrimSpace(slackEntities.Replace(text))
	if botUserID != "" {
		for _, m := range []string{"<@" + botUserID + ">", "<@" + botUserID + "|"} {
			if rest, ok := strings.CutPrefix(s, m); ok {
				if !strings.HasSuffix(m, ">") {
					if i := strings.IndexByte(rest, '>'); i >= 0 {
						rest = rest[i+1:]
					}
				}
				s = strings.TrimSpace(rest)
				break
			}
		}
	}
	return s
}

// folded is text compared without case, width or repeated spaces. end[i] is
// the byte offset in the original text just after the rune that made r[i].
type folded struct {
	r   []rune
	end []int
}

func fold(s string) folded {
	var f folded
	space := false
	for i, c := range s {
		end := i + utf8.RuneLen(c)
		switch {
		case c == 0x3000:
			c = ' '
		case c >= 0xFF01 && c <= 0xFF5E: // full-width ASCII
			c -= 0xFEE0
		}
		if unicode.IsSpace(c) {
			if space {
				f.end[len(f.end)-1] = end
				continue
			}
			c, space = ' ', true
		} else {
			space = false
		}
		f.r = append(f.r, unicode.ToLower(c))
		f.end = append(f.end, end)
	}
	return f
}

// isWord reports ASCII letters and digits: a keyword of them does not match
// in the middle of a word ("entry" is not in "entryx"). Japanese has no word
// breaks, so "相談" matches "相談したい".
func isWord(c rune) bool {
	return c < utf8.RuneSelf && (unicode.IsLetter(c) || unicode.IsDigit(c))
}

func at(t, k []rune, i int) bool {
	if i < 0 || i+len(k) > len(t) {
		return false
	}
	for j := range k {
		if t[i+j] != k[j] {
			return false
		}
	}
	if i > 0 && isWord(k[0]) && isWord(t[i-1]) {
		return false
	}
	if n := i + len(k); n < len(t) && isWord(k[len(k)-1]) && isWord(t[n]) {
		return false
	}
	return true
}

// startsWith reports whether body starts with keyword and returns the rest.
func startsWith(body, keyword string) (string, bool) {
	t, k := fold(body), fold(strings.TrimSpace(keyword)).r
	if len(k) == 0 || !at(t.r, k, 0) {
		return "", false
	}
	return strings.TrimSpace(body[t.end[len(k)-1]:]), true
}

// contains reports whether body contains keyword.
func contains(body, keyword string) bool {
	t, k := fold(body), fold(strings.TrimSpace(keyword)).r
	if len(k) == 0 {
		return false
	}
	for i := 0; i+len(k) <= len(t.r); i++ {
		if at(t.r, k, i) {
			return true
		}
	}
	return false
}

// ShadowedBy reports whether every post that matches later also matches
// earlier, so later can never run while earlier comes first and is on.
// Regexes are only compared for equality.
func ShadowedBy(earlier, later *Route) bool {
	if earlier.Channel != later.Channel {
		return false
	}
	ek, lk := earlier.Kind(), later.Kind()
	switch {
	case ek == "" || lk == "":
		return false
	case ek == KindAllPosts:
		return true
	case ek == KindMatch || lk == KindMatch || lk == KindAllPosts:
		return ek == lk && earlier.Match == later.Match
	case lk == KindStartsWith:
		_, ok := earlier.matchBody(strings.TrimSpace(later.StartsWith))
		return ok
	default: // later contains: only an earlier contains can cover it
		return ek == KindContains && contains(later.Contains, earlier.Contains)
	}
}
