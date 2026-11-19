package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReactionInput(t *testing.T) {
	r := &InputReaction{Name: "ticket", User: "U1", Action: "removed"}
	in := Input{Route: "r", Turn: 2, Channel: "C1", ThreadTS: "1.1", Reaction: r,
		Messages: []InputMessage{{TS: "1.1", User: "U9", Text: "search is broken", Reaction: r}}}

	// A program reads the reaction once, at the top level.
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, `"reaction":{"name":"ticket","user":"U1","action":"removed"}`) || strings.Count(got, `"reaction"`) != 1 {
		t.Errorf("json = %s", got)
	}

	// An agent is told per message, before the post.
	p := ClaudePrompt(in)
	tag := `<slack_reaction name="ticket" action="removed" user="U1" on="1.1" />`
	if !strings.Contains(p, tag) || strings.Index(p, tag) > strings.Index(p, "<slack_message") {
		t.Errorf("prompt:\n%s", p)
	}
	if !strings.Contains(p, "A reaction on a Slack post started this turn") {
		t.Errorf("prompt does not say a reaction started the turn:\n%s", p)
	}

	// Without a reaction the prompt is what it was.
	in.Reaction, in.Messages[0].Reaction = nil, nil
	if p := ClaudePrompt(in); strings.Contains(p, "slack_reaction") || !strings.Contains(p, "A new message arrived in the Slack thread.") {
		t.Errorf("prompt without a reaction:\n%s", p)
	}
}
