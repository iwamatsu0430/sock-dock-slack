// Command mkicon writes SockDock's icons.
//
//	mkicon <dir.iconset>     the app icon as a macOS .iconset (convert it with `iconutil -c icns`)
//	mkicon -slack <file.png> the 1024 px icon for the Slack app's settings (assets/slack-icon.png)
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/iwamatsu0430/sock-dock-slack/internal/brand"
)

func main() {
	switch {
	case len(os.Args) == 3 && os.Args[1] == "-slack":
		check(os.WriteFile(os.Args[2], brand.SlackIconPNG(1024), 0o644))
	case len(os.Args) == 2:
		iconset(os.Args[1])
	default:
		fmt.Fprintln(os.Stderr, "usage: mkicon <dir.iconset> | mkicon -slack <file.png>")
		os.Exit(2)
	}
}

func iconset(dir string) {
	check(os.MkdirAll(dir, 0o755))
	for _, s := range []int{16, 32, 128, 256, 512} {
		for _, scale := range []int{1, 2} {
			name := fmt.Sprintf("icon_%dx%d.png", s, s)
			if scale == 2 {
				name = fmt.Sprintf("icon_%dx%d@2x.png", s, s)
			}
			check(os.WriteFile(filepath.Join(dir, name), brand.AppIconPNG(s*scale), 0o644))
		}
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
