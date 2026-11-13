package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/iwamatsu0430/sock-dock-slack/internal/brand"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/store"
)

// monitor follows the gateway's event stream, keeps the menu bar icon and
// menu current, forwards events to the UI and raises notifications.
type monitor struct {
	svc  *AppService
	tray *application.SystemTray

	lastState      brand.TrayState
	lastMenu       string
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
	evLocale   = "sockdock:locale"
)

func (m *monitor) run() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	go func() {
		// Keep elapsed times in the menu roughly current while something runs.
		for range tick.C {
			if snap := m.svc.GetSnapshot(); snap.Status != nil && len(snap.Status.Running) > 0 {
				m.refresh()
			}
		}
	}()
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

// refresh fetches a snapshot, updates the icon and menu, and tells the UI.
func (m *monitor) refresh() {
	snap := m.svc.GetSnapshot()
	m.svc.app.Event.Emit(evSnapshot, snap)

	state := brand.TrayDisconnected
	switch {
	case snap.Running && snap.Status.Connected && len(snap.Status.Running) > 0:
		state = brand.TrayBusy
	case snap.Running && snap.Status.Connected:
		state = brand.TrayConnected
	}
	if state != m.lastState {
		m.tray.SetTemplateIcon(brand.TrayIcon(state))
		m.lastState = state
	}
	m.updateMenu(snap)

	// If another sockdock (the CLI service) was running and has gone away, take over.
	if !snap.Running && snap.Gateway.State == GatewayExternal {
		go m.svc.emb.Start()
	}

	// Notify once when Slack has been unreachable for 2 minutes, unless setup
	// is not finished or the user disconnected.
	quiet := snap.Gateway.State == GatewayStopped || snap.Gateway.State == GatewaySetup
	if state == brand.TrayDisconnected && !quiet {
		if m.disconnectedAt.IsZero() {
			m.disconnectedAt = time.Now()
		}
		if !m.notifiedDown && time.Since(m.disconnectedAt) > 2*time.Minute && m.svc.GetPrefs().Notifications {
			notify("SockDock", m.svc.tr("notify.down"))
			m.notifiedDown = true
		}
	} else {
		m.disconnectedAt, m.notifiedDown = time.Time{}, false
	}
}

type menuEntry struct {
	label   string
	right   string
	enabled bool
	onClick func()
}

// updateMenu rebuilds the menu bar menu when its content changed:
// state · running · recent runs · open · settings · quit.
func (m *monitor) updateMenu(snap Snapshot) {
	s := m.svc
	var head string
	switch {
	case snap.Gateway.State == GatewaySetup || snap.Gateway.State == GatewayStopped:
		head = s.tr("menu.setup")
	case snap.Running && snap.Status.Connected:
		head = "● " + s.tr("menu.connected")
	default:
		head = s.tr("menu.disconnected")
	}
	groups := [][]menuEntry{{{label: head}}}

	if snap.Status != nil && len(snap.Status.Running) > 0 {
		g := []menuEntry{{label: s.tr("menu.running")}}
		for _, rt := range snap.Status.Running {
			link := rt.Permalink
			g = append(g, menuEntry{label: s.labelFor(rt.Route), right: m.elapsed(rt.StartedAt), enabled: link != "",
				onClick: func() { _ = s.OpenURL(link) }})
		}
		groups = append(groups, g)
	}
	var recent []store.Turn
	for _, t := range s.RecentTurns(10) {
		if t.Status != store.StatusRunning && len(recent) < 5 {
			recent = append(recent, t)
		}
	}
	if len(recent) > 0 {
		g := []menuEntry{{label: s.tr("menu.recent")}}
		for _, t := range recent {
			name := t.Route
			g = append(g, menuEntry{label: s.labelFor(t.Route), right: s.tr("status."+t.Status) + " · " + t.StartedAt.Format("15:04"),
				enabled: true, onClick: func() { s.OpenMain(name) }})
		}
		groups = append(groups, g)
	}
	groups = append(groups,
		[]menuEntry{
			{label: s.tr("menu.open"), enabled: true, onClick: func() { s.OpenMain("") }},
			{label: s.tr("menu.settings"), right: "⌘,", enabled: true, onClick: s.OpenSettings},
		},
		[]menuEntry{{label: s.tr("menu.quit"), right: "⌘Q", enabled: true, onClick: func() { s.app.Quit() }}},
	)

	var sig strings.Builder
	for _, g := range groups {
		for _, e := range g {
			fmt.Fprintf(&sig, "%s|%s|%v;", e.label, e.right, e.enabled)
		}
		sig.WriteString("/")
	}
	if sig.String() == m.lastMenu {
		return
	}
	m.lastMenu = sig.String()

	menu := application.NewMenu()
	for i, g := range groups {
		if i > 0 {
			menu.AddSeparator()
		}
		for _, e := range g {
			label := e.label
			if e.right != "" && e.right != "⌘," && e.right != "⌘Q" {
				label += "    " + e.right
			}
			item := menu.Add(label).SetEnabled(e.enabled)
			switch e.right {
			case "⌘,":
				item.SetAccelerator("CmdOrCtrl+,")
			case "⌘Q":
				item.SetAccelerator("CmdOrCtrl+q")
			}
			if e.onClick != nil {
				f := e.onClick
				item.OnClick(func(*application.Context) { f() })
			}
		}
	}
	m.tray.SetMenu(menu)
}

func (m *monitor) elapsed(start time.Time) string {
	d := int(time.Since(start).Seconds())
	if d < 60 {
		return m.svc.tr("menu.seconds", d)
	}
	return m.svc.tr("menu.minutes", d/60, d%60)
}

// relabel forces the menu to be rebuilt (after a language change).
func (m *monitor) relabel() {
	m.lastMenu = ""
	m.refresh()
}

func (m *monitor) maybeNotifyTurn(e gateway.Event) {
	data, ok := e.Data.(map[string]any)
	if !ok {
		return
	}
	status, _ := data["status"].(string)
	if (status != store.StatusError && status != store.StatusTimeout) || !m.svc.GetPrefs().Notifications {
		return
	}
	route, _ := data["route"].(string)
	notify("SockDock", m.svc.tr("notify.failed", m.svc.labelFor(route)))
}
