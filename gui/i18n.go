package main

import "fmt"

// Native strings (menu bar menu, window titles, notifications). Web UI
// strings live in frontend/src/lib/i18n. Wording follows docs/ia.md.
var nativeText = map[string]map[string]string{
	"en": {
		"label.keyword":       "%s \u201c%s\u201d",
		"label.regex":         "%s /%s/",
		"menu.connected":      "Connected to Slack",
		"menu.disconnected":   "Can't connect to Slack",
		"menu.setup":          "Not connected to Slack",
		"menu.running":        "Running",
		"menu.recent":         "Recent runs",
		"menu.open":           "Open SockDock",
		"menu.settings":       "Settings…",
		"menu.quit":           "Quit SockDock",
		"menu.seconds":        "%ds",
		"menu.minutes":        "%dm %ds",
		"status.ok":           "Succeeded",
		"status.error":        "Failed",
		"status.timeout":      "Timed out",
		"status.stopped":      "Stopped",
		"status.interrupted":  "Stopped",
		"status.running":      "Running",
		"notify.failed":       "%s failed",
		"notify.down":         "Can't connect to Slack",
		"notify.mcp":          "%s was changed through MCP",
		"err.disconnectEnv":   "The tokens are set in an environment variable or secrets_file, so SockDock can't remove them.",
		"err.noClaudeDesktop": "Claude Desktop wasn't found.",
		"err.resetEnv":        "The tokens are set in an environment variable or secrets_file, so SockDock can't reset itself.",
		"err.resetService":    "SockDock is running as a service, so it can't be reset. Stop the service first.",
	},
	"ja": {
		"label.keyword":       "%s「%s」",
		"label.regex":         "%s /%s/",
		"menu.connected":      "Slack に接続済み",
		"menu.disconnected":   "Slack に接続できません",
		"menu.setup":          "Slack に接続していません",
		"menu.running":        "処理中",
		"menu.recent":         "最近の処理",
		"menu.open":           "SockDock を開く",
		"menu.settings":       "設定…",
		"menu.quit":           "SockDock を終了",
		"menu.seconds":        "%d秒",
		"menu.minutes":        "%d分%d秒",
		"status.ok":           "成功",
		"status.error":        "失敗",
		"status.timeout":      "時間切れ",
		"status.stopped":      "停止",
		"status.interrupted":  "停止",
		"status.running":      "処理中",
		"notify.failed":       "%s の処理が失敗しました",
		"notify.down":         "Slack に接続できません",
		"notify.mcp":          "MCP から %s を変更しました",
		"err.disconnectEnv":   "トークンが環境変数か secrets_file に書かれているため、SockDock からは解除できません。",
		"err.noClaudeDesktop": "Claude Desktop が見つかりません。",
		"err.resetEnv":        "トークンが環境変数か secrets_file に書かれているため、SockDock からは初期状態に戻せません。",
		"err.resetService":    "サービスで動いているため、初期状態に戻せません。先にサービスを止めてください。",
	},
}

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
