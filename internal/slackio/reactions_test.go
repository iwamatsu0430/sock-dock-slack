package slackio

import (
	"testing"

	"github.com/slack-go/slack"
)

func TestPostOf(t *testing.T) {
	msg := func(f func(*slack.Msg)) slack.Message {
		m := slack.Msg{Timestamp: "1.1", User: "U1", Text: "hello"}
		f(&m)
		return slack.Message{Msg: m}
	}
	cases := []struct {
		name string
		m    slack.Message
		ok   bool
		text string
	}{
		{"a post", msg(func(*slack.Msg) {}), true, "hello"},
		{"a post that has replies", msg(func(m *slack.Msg) { m.ThreadTimestamp = "1.1" }), true, "hello"},
		{"a reply shown in the channel", msg(func(m *slack.Msg) { m.ThreadTimestamp = "0.9"; m.SubType = "thread_broadcast" }), false, ""},
		{"someone joined", msg(func(m *slack.Msg) { m.SubType = "channel_join" }), false, ""},
		{"another app's post with attachments only", msg(func(m *slack.Msg) {
			m.User, m.BotID, m.SubType, m.Text = "", "B1", "bot_message", ""
			m.Attachments = []slack.Attachment{{Title: "api-server", Text: "5xx above 5%", Fallback: "ignored"}, {Fallback: "see dashboard"}}
		}), true, "api-server\n5xx above 5%\nsee dashboard"},
	}
	for _, c := range cases {
		got, ok, err := postOf("C1", c.m)
		if err != nil || ok != c.ok || got.Text != c.text {
			t.Errorf("%s: ok %v, text %q, err %v", c.name, ok, got.Text, err)
		}
		if ok && (got.Channel != "C1" || got.TS != "1.1") {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}
