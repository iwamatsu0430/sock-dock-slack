// Package brand draws SockDock's icons in code: a wall-outlet plate with two
// slots (one socket, many plugs). No image files are needed.
package brand

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// TrayState selects the menu bar icon variant.
type TrayState int

const (
	TrayConnected TrayState = iota
	TrayBusy
	TrayDisconnected
)

// TrayIcon returns a macOS template PNG (black + alpha) at 2x (36x36 px for
// an 18pt menu bar icon).
func TrayIcon(s TrayState) []byte {
	const size = 36
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	alpha := 1.0
	if s == TrayDisconnected {
		alpha = 0.45
	}
	paint(img, func(x, y float64) float64 {
		// Plate outline.
		plate := math.Abs(roundedRect(x, y, 17, 19, 13, 13, 5)) - 1.6
		cov := coverage(plate)
		// Two slots.
		cov = math.Max(cov, coverage(roundedRect(x, y, 12.5, 18, 1.9, 4.6, 1.9)))
		cov = math.Max(cov, coverage(roundedRect(x, y, 21.5, 18, 1.9, 4.6, 1.9)))
		// Ground hole.
		cov = math.Max(cov, coverage(circle(x, y, 17, 25.5, 1.8)))
		if s == TrayBusy {
			// Clear a ring around the badge, then draw it.
			ring := coverage(circle(x, y, 30, 7, 6.5))
			cov *= 1 - ring
			cov = math.Max(cov, coverage(circle(x, y, 30, 7, 4.5)))
		}
		if s == TrayDisconnected {
			// Diagonal slash, with a gap cut out of the plate around it.
			slash := segment(x, y, 6, 32, 30, 4)
			cov *= 1 - coverage(slash-2.6)
			cov = math.Max(cov, coverage(slash-1.4)/alpha)
		}
		return cov * alpha
	}, color.NRGBA{0, 0, 0, 255})
	return encode(img)
}

// AppIcon renders the application icon at the given size.
func AppIcon(size int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / 1024
	top := color.NRGBA{0x4f, 0x46, 0xe5, 255}
	bottom := color.NRGBA{0x1e, 0x1b, 0x4b, 255}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x, y := (float64(px)+0.5)/s, (float64(py)+0.5)/s
			// macOS-style squircle-ish background with margin.
			bg := coverage(roundedRect(x, y, 512, 512, 412, 412, 92) * s)
			if bg <= 0 {
				continue
			}
			t := y / 1024
			c := lerp(top, bottom, t)
			// White outlet glyph.
			glyph := coverage((math.Abs(roundedRect(x, y, 512, 540, 250, 250, 90)) - 34) * s)
			glyph = math.Max(glyph, coverage(roundedRect(x, y, 420, 500, 34, 92, 34)*s))
			glyph = math.Max(glyph, coverage(roundedRect(x, y, 604, 500, 34, 92, 34)*s))
			glyph = math.Max(glyph, coverage(circle(x, y, 512, 660, 38)*s))
			c = lerp(c, color.NRGBA{255, 255, 255, 255}, glyph)
			c.A = uint8(255 * bg)
			img.SetNRGBA(px, py, c)
		}
	}
	return img
}

// AppIconPNG is AppIcon encoded as PNG.
func AppIconPNG(size int) []byte { return encode(AppIcon(size)) }

func paint(img *image.NRGBA, f func(x, y float64) float64, c color.NRGBA) {
	b := img.Bounds()
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			a := math.Min(1, math.Max(0, f(float64(px)+0.5, float64(py)+0.5)))
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{c.R, c.G, c.B, uint8(a * 255)})
			}
		}
	}
}

// roundedRect is the signed distance to a rounded rectangle centred at
// (cx, cy) with half-size (hx, hy) and corner radius r.
func roundedRect(x, y, cx, cy, hx, hy, r float64) float64 {
	qx := math.Abs(x-cx) - (hx - r)
	qy := math.Abs(y-cy) - (hy - r)
	outside := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	inside := math.Min(math.Max(qx, qy), 0)
	return outside + inside - r
}

func circle(x, y, cx, cy, r float64) float64 { return math.Hypot(x-cx, y-cy) - r }

// segment is the distance to the line segment (ax, ay)-(bx, by).
func segment(x, y, ax, ay, bx, by float64) float64 {
	px, py := x-ax, y-ay
	dx, dy := bx-ax, by-ay
	h := math.Max(0, math.Min(1, (px*dx+py*dy)/(dx*dx+dy*dy)))
	return math.Hypot(px-dx*h, py-dy*h)
}

// coverage turns a signed distance (in pixels) into anti-aliased coverage.
func coverage(d float64) float64 { return math.Max(0, math.Min(1, 0.5-d)) }

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	t = math.Max(0, math.Min(1, t))
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t) }
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), m(a.A, b.A)}
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
