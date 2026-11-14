// Package event defines the normalized Slack message passed between packages.
package event

// Message is a Slack message event reduced to what sockdock needs.
type Message struct {
	Channel  string `json:"channel"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts,omitempty"`
	User     string `json:"user,omitempty"`
	BotID    string `json:"bot_id,omitempty"`
	SubType  string `json:"subtype,omitempty"`
	Text     string `json:"text"`
	// ChannelType is Slack's channel_type: "channel", "group", "im" or "mpim".
	ChannelType string `json:"channel_type,omitempty"`
	// Files are the files attached to the message (docs/design.md §3.6).
	Files []File `json:"files,omitempty"`
}

// File is a file attached to a message, as Slack describes it in the event.
type File struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Mimetype string `json:"mimetype,omitempty"`
	Size     int    `json:"size,omitempty"`
	// Mode is Slack's mode: "hosted", "snippet", "external", "post", ...
	Mode       string `json:"mode,omitempty"`
	IsExternal bool   `json:"is_external,omitempty"`
	// URL is url_private_download.
	URL string `json:"url,omitempty"`
	// FileAccess is "check_file_info" when Slack left the details out of
	// the event (Slack Connect); they are then read with files.info.
	FileAccess string `json:"file_access,omitempty"`
}

// IsIM reports whether the message is a DM to the Slack app.
func (m Message) IsIM() bool {
	return m.ChannelType == "im" || (m.ChannelType == "" && len(m.Channel) > 0 && m.Channel[0] == 'D')
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
