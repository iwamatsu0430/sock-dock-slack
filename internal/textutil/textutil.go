// Package textutil formats handler output for Slack.
package textutil

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// DefaultLimit is the maximum characters per Slack message we post.
const DefaultLimit = 3900

const fence = "```"

// Split breaks text into chunks of at most limit characters, preferring blank
// lines, then newlines, then spaces. A code block cut in the middle is closed
// at the end of one chunk and reopened at the start of the next.
func Split(text string, limit int) []string {
	if limit <= len(fence)*2+2 {
		limit = DefaultLimit
	}
	text = strings.TrimRight(text, "\n ")
	var out []string
	for utf8.RuneCountInString(text) > limit {
		// Leave room for a closing fence.
		budget := limit - len(fence) - 1
		cut := byteIndexOfRune(text, budget)
		head := text[:cut]
		if i := strings.LastIndex(head, "\n\n"); i > len(head)/2 {
			cut = i
		} else if i := strings.LastIndex(head, "\n"); i > len(head)/2 {
			cut = i
		} else if i := strings.LastIndex(head, " "); i > len(head)/2 {
			cut = i
		}
		chunk := strings.TrimRight(text[:cut], "\n ")
		rest := strings.TrimLeft(text[cut:], "\n")
		if strings.Count(chunk, fence)%2 == 1 {
			chunk += "\n" + fence
			rest = fence + "\n" + rest
		}
		out = append(out, chunk)
		text = rest
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, text)
	}
	return out
}

func byteIndexOfRune(s string, n int) int {
	i := 0
	for pos := range s {
		if i == n {
			return pos
		}
		i++
	}
	return len(s)
}

var (
	heading  = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
	boldStar = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	boldUnd  = regexp.MustCompile(`__([^_\n]+?)__`)
	mdLink   = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^)\s]+)\)`)
)

// ToMrkdwn converts the common Markdown constructs that Slack does not render
// (headings, **bold**, __bold__, [text](url)) into Slack mrkdwn. Code blocks
// and inline code are left untouched.
func ToMrkdwn(text string) string {
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), fence) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := heading.FindStringSubmatch(line); m != nil {
			line = "*" + strings.Trim(m[1], "*") + "*"
		}
		lines[i] = mapOutsideInlineCode(line, func(s string) string {
			s = boldStar.ReplaceAllString(s, "*$1*")
			s = boldUnd.ReplaceAllString(s, "*$1*")
			s = mdLink.ReplaceAllString(s, "<$2|$1>")
			return s
		})
	}
	return strings.Join(lines, "\n")
}

// mapOutsideInlineCode applies f to the parts of line outside `inline code`.
func mapOutsideInlineCode(line string, f func(string) string) string {
	parts := strings.Split(line, "`")
	if len(parts)%2 == 0 { // unbalanced backticks: leave the line alone
		return line
	}
	for i := 0; i < len(parts); i += 2 {
		parts[i] = f(parts[i])
	}
	return strings.Join(parts, "`")
}
