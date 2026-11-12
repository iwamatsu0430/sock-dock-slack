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
	openItem := menu.Add(svc.tr("menu.open")).OnClick(func(*application.Context) { svc.OpenWindow("overview") })
	reloadItem := menu.Add(svc.tr("menu.reload")).OnClick(func(*application.Context) { _ = svc.Reload() })
	menu.AddSeparator()
	quitItem := menu.Add(svc.tr("menu.quit")).OnClick(func(*application.Context) { app.Quit() })
	// Labels depend on the language and on whether quitting stops the gateway.
	relabel := func() {
		openItem.SetLabel(svc.tr("menu.open"))
		reloadItem.SetLabel(svc.tr("menu.reload"))
		if svc.emb.State().State == GatewayRunning {
			quitItem.SetLabel(svc.tr("menu.quitStops"))
		} else {
			quitItem.SetLabel(svc.tr("menu.quit"))
		}
		menu.Update()
	}
	svc.onLocale = relabel
	// Left click toggles the popover; right click opens this menu.
	tray.SetMenu(menu)

	mon := newMonitor(svc, tray)
	svc.emb.onChange = func() {
		relabel()
		go mon.refresh()
	}
	go mon.run()

	// Run the gateway inside the app. Apps opened from Finder get a minimal
	// PATH, so use the login shell's PATH for handlers (claude, uv, ...).
	go func() {
		_ = os.Setenv("PATH", loginShellPath())
		_ = svc.ensureConfig()
		svc.emb.Start()
	}()

	if len(os.Args) > 1 && os.Args[1] == "--show" {
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			svc.OpenWindow("overview")
		})
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
