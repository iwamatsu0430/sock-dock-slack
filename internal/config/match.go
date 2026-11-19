package config

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Condition kinds: which top-level posts a route reacts to.
// A route has exactly one.
const (
	KindStartsWith = "starts_with"
	KindContains   = "contains"
	KindMatch      = "match"
	KindAllPosts   = "all_posts"
	// KindReaction reacts to an emoji reaction added to a post instead of
	// the post's text.
	KindReaction = "reaction"
)

// Reaction actions: what happened to the reaction that started a run.
const (
	ReactionAdded   = "added"
	ReactionRemoved = "removed"
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
	case NormalizeReaction(r.Reaction) != "":
		return KindReaction
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
	case KindReaction:
		return NormalizeReaction(r.Reaction)
	}
	return ""
}

var skinTone = regexp.MustCompile(`::skin-tone-\d+$`)

// NormalizeReaction returns an emoji name the way Slack's reaction events
// carry it: lower case, without the colons around it, without a skin tone
// ("+1::skin-tone-2" is "+1"), and under the name Slack sends rather than
// another name of the same emoji ("thumbsup" is "+1").
func NormalizeReaction(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = skinTone.ReplaceAllString(strings.Trim(s, ":"), "")
	s = strings.Trim(s, ":")
	if name, ok := emojiAliases[s]; ok {
		return name
	}
	return s
}

// Condition describes the condition as written in the config ("starts_with:
// entry", with ", mention_only" for a rule that needs a mention and
// ", reaction_removed" for a reaction rule that also runs on removal).
func (r *Route) Condition() string {
	c := r.Kind()
	if c != "" && c != KindAllPosts {
		c += ": " + r.Pattern()
	}
	if r.MentionOnly {
		c += ", mention_only"
	}
	if r.ReactionRemoved && r.Kind() == KindReaction {
		c += ", reaction_removed"
	}
	return c
}

// conditionProblem checks that exactly one condition is set and compiles a regex.
func (r *Route) conditionProblem() string {
	n := 0
	reaction := NormalizeReaction(r.Reaction)
	for _, set := range []bool{strings.TrimSpace(r.StartsWith) != "", strings.TrimSpace(r.Contains) != "", r.Match != "", r.AllPosts, reaction != ""} {
		if set {
			n++
		}
	}
	switch {
	case n == 0 && strings.TrimSpace(r.Reaction) != "":
		return fmt.Sprintf("reaction: %q is not an emoji name (like eyes)", r.Reaction)
	case n == 0:
		return "one of starts_with, contains, match, all_posts or reaction is required"
	case n > 1:
		return "use only one of starts_with, contains, match, all_posts and reaction"
	}
	if reaction != "" && strings.ContainsAny(reaction, ": \t\n") {
		return fmt.Sprintf("reaction: %q is not an emoji name (like eyes)", r.Reaction)
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
	if r.StartsWith != "" || r.Contains != "" || r.AllPosts || r.Reaction != "" {
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
	// IM is true for a DM to the Slack app: it is matched against the rules
	// of channel "dm" (passing channel "dm" does the same).
	IM bool
	// Mentioned treats the post as mentioning the Slack app (for trying a
	// post without knowing the app's user ID).
	Mentioned bool
	// Skip reports routes to pass over (turned off).
	Skip func(name string) bool
	// ByUser passes over routes that User may not run (allowed_users). When
	// it is false, who posted is not considered.
	ByUser bool
	User   string
}

// Restricted reports whether only some people may run the route.
func (r *Route) Restricted() bool { return len(r.AllowedUsers) > 0 }

// Allows reports whether the Slack member may run the route.
func (r *Route) Allows(user string) bool {
	if !r.Restricted() {
		return true
	}
	for _, u := range r.AllowedUsers {
		if u == user {
			return user != ""
		}
	}
	return false
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
	if o.IM {
		channel = ChannelDM
	}
	mentioned := o.Mentioned || Mentions(text, o.BotUserID)
	for _, r := range c.Routes {
		if r.Channel != channel || (o.Skip != nil && o.Skip(r.Name)) || (o.ByUser && !r.Allows(o.User)) {
			continue
		}
		// A rule for mentions only is passed over, like one that is off,
		// when the post does not mention the Slack app.
		if r.MentionOnly && !mentioned {
			continue
		}
		if g, ok := r.matchBody(body); ok {
			return r, g
		}
	}
	return nil, nil
}

// MatchReaction finds the first reaction route for a reaction added to a
// post in the channel. Routes are tried in config order; Skip, ByUser/User
// (the person who reacted) and IM work as in MatchWith.
func (c *Config) MatchReaction(channel, name string, o MatchOptions) *Route {
	if o.IM {
		channel = ChannelDM
	}
	name = NormalizeReaction(name)
	if name == "" {
		return nil
	}
	for _, r := range c.Routes {
		if r.Channel != channel || r.Kind() != KindReaction || r.Pattern() != name {
			continue
		}
		if (o.Skip != nil && o.Skip(r.Name)) || (o.ByUser && !r.Allows(o.User)) {
			continue
		}
		return r
	}
	return nil
}

// matchBody matches a post body (see postBody). A reaction route never
// matches a post's text.
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

// Mentions reports whether Slack text mentions the Slack app anywhere.
func Mentions(text, botUserID string) bool {
	if botUserID == "" {
		return false
	}
	return strings.Contains(text, "<@"+botUserID+">") || strings.Contains(text, "<@"+botUserID+"|")
}

// PostBody is the text of a post as rules see it (see postBody).
func PostBody(text, botUserID string) string { return postBody(text, botUserID) }

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
// in the middle of a word ("review" is not in "reviewer"). Japanese has no word
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
// Regexes are only compared for equality. An earlier route that only some
// people may run lets everyone else's posts through, so it shadows nothing;
// likewise an earlier route for mentions only lets other posts through.
func ShadowedBy(earlier, later *Route) bool {
	if earlier.Channel != later.Channel || earlier.Restricted() || (earlier.MentionOnly && !later.MentionOnly) {
		return false
	}
	ek, lk := earlier.Kind(), later.Kind()
	switch {
	case ek == "" || lk == "":
		return false
	case ek == KindReaction || lk == KindReaction:
		// A reaction and a post's text never compete: an "all posts" rule
		// does not see other apps' posts, which a reaction rule runs for.
		return ek == lk && earlier.Pattern() == later.Pattern()
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
