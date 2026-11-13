// Command sockdock-gui is the menu bar app for SockDock for Slack. It runs
// the gateway in-process and talks to it through the control API.
package main

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/iwamatsu0430/sock-dock-slack/internal/brand"
	"github.com/iwamatsu0430/sock-dock-slack/internal/gateway"
	"github.com/iwamatsu0430/sock-dock-slack/internal/paths"
)

//go:embed all:frontend/dist
var frontendFS embed.FS

// version is set at build time.
var version = "dev"

func init() {
	application.RegisterEvent[Snapshot](evSnapshot)
	application.RegisterEvent[gateway.Event](evActivity)
	application.RegisterEvent[string](evNavigate)
	application.RegisterEvent[string](evLocale)
}

// instanceID keeps a development copy (SOCKDOCK_DATA_DIR set) separate from
// the installed app.
func instanceID() string {
	id := "io.github.iwamatsu0430.sockdock.gui"
	if d := os.Getenv("SOCKDOCK_DATA_DIR"); d != "" {
		id += "." + fmt.Sprintf("%x", sha256.Sum256([]byte(d)))[:12]
	}
	return id
}

func main() {
	dist, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	dirs := paths.Default()
	svc := newAppService(dirs, version)

	app := application.New(application.Options{
		Name:        "SockDock",
		Description: "SockDock for Slack",
		Icon:        brand.AppIconPNG(256),
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac:         application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		LogLevel:    slog.LevelWarn,
		// Quitting the app stops the gateway it runs.
		OnShutdown: func() { svc.emb.Stop(15 * time.Second) },
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID(),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				svc.OpenMain("")
			},
		},
	})
	svc.app = app

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(brand.TrayIcon(brand.TrayDisconnected))
	tray.SetTooltip("SockDock")

	keys := map[string]func(application.Window){
		"CmdOrCtrl+,": func(application.Window) { svc.OpenSettings() },
		"CmdOrCtrl+w": func(w application.Window) { w.Hide() },
	}
	main := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:        "main",
		Title:       "SockDock",
		URL:         "/#/main",
		Width:       760,
		Height:      600,
		MinWidth:    600,
		MinHeight:   440,
		Hidden:      true,
		KeyBindings: keys,
	})
	settings := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          "settings",
		Title:         svc.tr("title.settings"),
		URL:           "/#/settings",
		Width:         520,
		Height:        460,
		Hidden:        true,
		DisableResize: true,
		KeyBindings:   keys,
	})
	// Closing a window only hides it; the app lives in the menu bar.
	for _, w := range []*application.WebviewWindow{main, settings} {
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			w.Hide()
			e.Cancel()
		})
	}
	svc.main, svc.settings = main, settings

	// Clicking the menu bar icon opens the menu (built by the monitor).
	mon := newMonitor(svc, tray)
	svc.onLocale = func() {
		settings.SetTitle(svc.tr("title.settings"))
		go mon.relabel()
	}
	svc.emb.onChange = func() { go mon.refresh() }
	go mon.run()

	// Run the gateway inside the app. Apps opened from Finder get a minimal
	// PATH, so use the login shell's PATH for handlers (claude, uv, ...).
	go func() {
		_ = os.Setenv("PATH", loginShellPath())
		_ = svc.ensureConfig()
		svc.emb.Start()
	}()

	// Show the window on first launch (setup) and when asked to.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if (len(os.Args) > 1 && os.Args[1] == "--show") || !svc.hasTokens() {
			svc.OpenMain("")
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
