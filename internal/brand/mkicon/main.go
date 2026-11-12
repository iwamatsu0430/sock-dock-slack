// Command mkicon writes the app icon as a macOS .iconset directory
// (convert it with `iconutil -c icns`).
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/iwamatsu0430/sock-dock-slack/internal/brand"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: mkicon <dir.iconset>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, s := range []int{16, 32, 128, 256, 512} {
		for _, scale := range []int{1, 2} {
			name := fmt.Sprintf("icon_%dx%d.png", s, s)
			if scale == 2 {
				name = fmt.Sprintf("icon_%dx%d@2x.png", s, s)
			}
			if err := os.WriteFile(filepath.Join(dir, name), brand.AppIconPNG(s*scale), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
}
