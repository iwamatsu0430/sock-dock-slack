// Command sockdock-gui is the menu bar app for SockDock for Slack. It is a thin
// client of the sockdock gateway's control API; the gateway itself runs as a
// separate background service.
package main

import (
	"embed"
	"io/fs"
	"log"
	"log/slog"
	"os"

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
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.iwamatsu0430.sockdock.gui",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				svc.OpenWindow("overview")
			},
		},
	})
	svc.app = app

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(brand.TrayIcon(brand.TrayDisconnected))
	tray.SetTooltip("SockDock")

	popover := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:            "tray",
		Title:           "SockDock",
		URL:             "/#/tray",
		Width:           380,
		Height:          520,
		Frameless:       true,
		AlwaysOnTop:     true,
		Hidden:          true,
		DisableResize:   true,
		HideOnEscape:    true,
		HideOnFocusLost: true,
		Mac: application.MacWindow{
			Backdrop: application.MacBackdropTranslucent,
		},
	})
	tray.AttachWindow(popover).WindowOffset(6)

	main := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "SockDock",
		URL:       "/#/main",
		Width:     1000,
		Height:    700,
		MinWidth:  760,
		MinHeight: 520,
		Hidden:    true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 40,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	})
	// Closing the window only hides it; the app lives in the menu bar.
	main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		main.Hide()
		e.Cancel()
	})
	svc.popover, svc.main = popover, main

	menu := app.NewMenu()
	menu.Add("Open SockDock…").OnClick(func(*application.Context) { svc.OpenWindow("overview") })
	menu.Add("Reload config").OnClick(func(*application.Context) { _ = svc.Reload() })
	menu.AddSeparator()
	menu.Add("Quit SockDock").OnClick(func(*application.Context) { app.Quit() })
	// Left click toggles the popover; right click opens this menu.
	tray.SetMenu(menu)

	mon := newMonitor(svc, tray)
	go mon.run()

	if len(os.Args) > 1 && os.Args[1] == "--show" {
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			svc.OpenWindow("overview")
		})
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
