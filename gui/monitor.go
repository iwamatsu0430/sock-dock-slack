package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/iwamatsu0430/sock-dock-slack/internal/brand"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
)

// monitor follows the gateway's event stream, keeps the tray icon current,
// forwards events to the UI and raises notifications.
type monitor struct {
	svc  *AppService
	tray *application.SystemTray

	lastState      brand.TrayState
	disconnectedAt time.Time
	notifiedDown   bool
}

func newMonitor(svc *AppService, tray *application.SystemTray) *monitor {
	return &monitor{svc: svc, tray: tray, lastState: -1}
}

// Events sent to the frontend.
const (
	evSnapshot = "sockdock:snapshot"
	evActivity = "sockdock:activity"
	evNavigate = "sockdock:navigate"
)

func (m *monitor) run() {
	for {
		ctx, cancel := context.WithCancel(context.Background())
		m.refresh()
		_ = m.svc.client.Events(ctx, func(e gateway.Event) {
			m.svc.app.Event.Emit(evActivity, e)
			if e.Type == "turn_finished" {
				m.maybeNotifyTurn(e)
			}
			m.refresh()
		})
		cancel()
		// Stream ended: the gateway stopped or is not running yet.
		m.refresh()
		time.Sleep(3 * time.Second)
	}
}

// refresh fetches a snapshot, updates the tray and tells the UI.
func (m *monitor) refresh() {
	snap := m.svc.GetSnapshot()
	m.svc.app.Event.Emit(evSnapshot, snap)

	state, tip := brand.TrayDisconnected, "SockDock: gateway not running"
	switch {
	case snap.Running && snap.Status.Connected && len(snap.Status.Running) > 0:
		state, tip = brand.TrayBusy, fmt.Sprintf("SockDock: %d running", len(snap.Status.Running))
	case snap.Running && snap.Status.Connected:
		state, tip = brand.TrayConnected, "SockDock: connected"
	case snap.Running:
		tip = "SockDock: not connected to Slack"
	}
	if state != m.lastState {
		m.tray.SetTemplateIcon(brand.TrayIcon(state))
		m.lastState = state
	}
	m.tray.SetTooltip(tip)

	// Notify once when the gateway has been down or disconnected for 2 minutes.
	if state == brand.TrayDisconnected {
		if m.disconnectedAt.IsZero() {
			m.disconnectedAt = time.Now()
		}
		if !m.notifiedDown && time.Since(m.disconnectedAt) > 2*time.Minute && m.svc.GetPrefs().Notifications {
			notify("SockDock", tip)
			m.notifiedDown = true
		}
	} else {
		m.disconnectedAt, m.notifiedDown = time.Time{}, false
	}
}

func (m *monitor) maybeNotifyTurn(e gateway.Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	status, _ := data["status"].(string)
	if status == "ok" || !m.svc.GetPrefs().Notifications {
		return
	}
	route, _ := data["route"].(string)
	notify("SockDock", fmt.Sprintf("Handler for %q ended with status: %s", route, status))
}
