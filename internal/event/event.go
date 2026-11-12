// Package event defines the normalized Slack message passed between packages.
package event

// File is the metadata of a file attached to a message. Files are never downloaded.
type File struct {
	Name       string `json:"name"`
	Mimetype   string `json:"mimetype"`
	URLPrivate string `json:"url_private"`
}

// Message is a Slack message event reduced to what sockdock needs.
type Message struct {
	Channel  string `json:"channel"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts,omitempty"`
	User     string `json:"user,omitempty"`
	BotID    string `json:"bot_id,omitempty"`
	SubType  string `json:"subtype,omitempty"`
	Text     string `json:"text"`
	Files    []File `json:"files,omitempty"`
}

// IsTopLevel reports whether the message starts a thread (is not a reply).
func (m Message) IsTopLevel() bool {
	return m.ThreadTS == "" || m.ThreadTS == m.TS
}

// ThreadRoot returns the ts of the thread this message belongs to.
func (m Message) ThreadRoot() string {
	if m.IsTopLevel() {
		return m.TS
	}
	return m.ThreadTS
}
