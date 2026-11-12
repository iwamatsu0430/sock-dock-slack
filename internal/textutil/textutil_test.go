package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitShortTextUnchanged(t *testing.T) {
	got := Split("hello", 100)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("%q", got)
	}
}

func TestSplitPrefersParagraphsAndRespectsLimit(t *testing.T) {
	para := strings.Repeat("あ", 30)
	text := strings.Join([]string{para, para, para, para}, "\n\n")
	chunks := Split(text, 70)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if n := utf8.RuneCountInString(c); n > 70 {
			t.Errorf("chunk too long (%d): %q", n, c)
		}
		if strings.HasPrefix(c, "\n") || strings.HasSuffix(c, "\n") {
			t.Errorf("chunk not trimmed: %q", c)
		}
	}
	if strings.Join(chunks, "\n\n") != text {
		t.Errorf("content lost or reordered")
	}
}

func TestSplitReopensCodeFence(t *testing.T) {
	code := "```\n" + strings.Repeat("line of code\n", 20) + "```"
	chunks := Split(code, 80)
	if len(chunks) < 2 {
		t.Fatal("expected a split")
	}
	for i, c := range chunks {
		if strings.Count(c, "```")%2 != 0 {
			t.Errorf("chunk %d has unbalanced fences: %q", i, c)
		}
	}
}

func TestToMrkdwn(t *testing.T) {
	in := "# Title\nSome **bold** and __also__ and [link](https://example.com).\n`**not bold**`\n```\n**code**\n# not heading\n```"
	want := "*Title*\nSome *bold* and *also* and <https://example.com|link>.\n`**not bold**`\n```\n**code**\n# not heading\n```"
	if got := ToMrkdwn(in); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
