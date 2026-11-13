package brand

import (
	"bytes"
	"image/png"
	"testing"
)

func TestTrayIcons(t *testing.T) {
	seen := map[string]TrayState{}
	for _, s := range []TrayState{TrayConnected, TrayBusy, TrayDisconnected} {
		b := TrayIcon(s)
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if r := img.Bounds(); r.Dx() != 36 || r.Dy() != 36 {
			t.Errorf("state %d: size %v, want 36x36", s, r)
		}
		if prev, ok := seen[string(b)]; ok {
			t.Errorf("states %d and %d draw the same icon", prev, s)
		}
		seen[string(b)] = s
	}
}

func TestTiles(t *testing.T) {
	app, slack := AppIcon(256), SlackIcon(256)
	// The app icon keeps the macOS grid margin; the Slack icon is full bleed.
	if _, _, _, a := app.At(4, 4).RGBA(); a != 0 {
		t.Errorf("app icon corner alpha = %d, want transparent", a)
	}
	if _, _, _, a := slack.At(4, 128).RGBA(); a == 0 {
		t.Error("slack icon edge is transparent, want full bleed")
	}
	// Mouth: just inside the right edge at mid-height shows the background.
	if c := slack.At(240, 128); c != indigo {
		t.Errorf("mouth colour = %v, want %v", c, indigo)
	}
	// Eyes are cut out of the white face.
	if c := slack.At(90, 109); c != indigo { // left eye: face (26, 38)
		t.Errorf("eye colour = %v, want %v", c, indigo)
	}
}
