package slackio

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/iwamatsu0430/sock-dock-slack/internal/event"
)

// ConnState is reported whenever the Socket Mode connection changes.
type ConnState struct {
	Connected bool
	Err       error
}

// Listener keeps one Socket Mode connection open, reconnecting with backoff,
// and delivers message events.
type Listener struct {
	client    *slack.Client
	log       *slog.Logger
	onMessage func(event.Message)
	onState   func(ConnState)
	reconnect chan struct{}
}

// NewListener creates a listener. onMessage is called from the listener
// goroutine and must not block for long.
func NewListener(client *slack.Client, log *slog.Logger, onMessage func(event.Message), onState func(ConnState)) *Listener {
	return &Listener{
		client:    client,
		log:       log,
		onMessage: onMessage,
		onState:   onState,
		reconnect: make(chan struct{}, 1),
	}
}

// Reconnect asks the listener to drop the current connection and open a new one.
func (l *Listener) Reconnect() {
	select {
	case l.reconnect <- struct{}{}:
	default:
	}
}

// Run blocks until ctx is cancelled.
func (l *Listener) Run(ctx context.Context) {
	backoff := time.Second
	for {
		cctx, cancel := context.WithCancel(ctx)
		sm := socketmode.New(l.client)
		go l.consume(cctx, sm)
		errc := make(chan error, 1)
		go func() { errc <- sm.RunContext(cctx) }()

		var err error
		forced := false
		select {
		case <-ctx.Done():
			cancel()
			<-errc
			return
		case <-l.reconnect:
			forced = true
			cancel()
			<-errc
		case err = <-errc:
			cancel()
		}
		l.onState(ConnState{Connected: false, Err: err})
		if forced {
			l.log.Info("reconnecting to Slack on request")
			backoff = time.Second
			continue
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			l.log.Warn("socket mode connection ended", "err", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-l.reconnect:
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
}

func (l *Listener) consume(ctx context.Context, sm *socketmode.Client) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-sm.Events:
			if !ok {
				return
			}
			l.handle(sm, evt)
		}
	}
}

func (l *Listener) handle(sm *socketmode.Client, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnecting:
		l.log.Debug("connecting to Slack")
	case socketmode.EventTypeConnected:
		l.log.Info("connected to Slack")
		l.onState(ConnState{Connected: true})
	case socketmode.EventTypeHello:
		l.onState(ConnState{Connected: true})
	case socketmode.EventTypeConnectionError, socketmode.EventTypeInvalidAuth:
		var err error
		if e, ok := evt.Data.(error); ok {
			err = e
		} else {
			err = errors.New(string(evt.Type))
		}
		l.log.Warn("slack connection problem", "type", evt.Type, "err", err)
		l.onState(ConnState{Connected: false, Err: err})
	case socketmode.EventTypeDisconnect:
		l.log.Info("slack asked to disconnect; reconnecting")
		l.onState(ConnState{Connected: false})
	case socketmode.EventTypeEventsAPI:
		if evt.Request != nil {
			if err := sm.Ack(*evt.Request); err != nil {
				l.log.Warn("ack failed", "err", err)
			}
		}
		api, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok || api.Type != slackevents.CallbackEvent {
			return
		}
		if m, ok := api.InnerEvent.Data.(*slackevents.MessageEvent); ok {
			l.onMessage(convert(m))
		}
	case socketmode.EventTypeInteractive, socketmode.EventTypeSlashCommand:
		// Not used yet, but acknowledge so Slack does not retry.
		if evt.Request != nil {
			_ = sm.Ack(*evt.Request)
		}
	}
}

func convert(m *slackevents.MessageEvent) event.Message {
	out := event.Message{
		Channel:  m.Channel,
		TS:       m.TimeStamp,
		ThreadTS: m.ThreadTimeStamp,
		User:     m.User,
		BotID:    m.BotID,
		SubType:  m.SubType,
		Text:     m.Text,
	}
	if m.Message != nil && out.Text == "" {
		out.Text = m.Message.Text
	}
	return out
}
