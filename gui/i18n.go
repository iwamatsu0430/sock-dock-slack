package main

import "fmt"

// Native strings (tray menu, tooltips, notifications). Web UI strings live in
// frontend/src/lib/i18n.
var nativeText = map[string]map[string]string{
	"en": {
		"menu.open":        "Open SockDock…",
		"menu.reload":      "Reload config",
		"menu.quit":        "Quit SockDock",
		"tip.notRunning":   "SockDock: gateway not running",
		"tip.disconnected": "SockDock: not connected to Slack",
		"tip.connected":    "SockDock: connected",
		"tip.running":      "SockDock: %d running",
		"notify.failed":    "Handler for %q ended with status: %s",
	},
	"ja": {
		"menu.open":        "SockDock を開く…",
		"menu.reload":      "設定を再読み込み",
		"menu.quit":        "SockDock を終了",
		"tip.notRunning":   "SockDock: ゲートウェイが動いていません",
		"tip.disconnected": "SockDock: Slack に接続していません",
		"tip.connected":    "SockDock: 接続中",
		"tip.running":      "SockDock: %d 件を処理中",
		"notify.failed":    "ルート %q の処理が「%s」で終わりました",
	},
}

// tr returns a native string in the current language.
func (a *AppService) tr(key string, args ...any) string {
	s, ok := nativeText[a.Locale()][key]
	if !ok {
		s = nativeText["en"][key]
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
