// Package brand draws SockDock's icons in code, so no image files are needed.
//
// The mark is an outlet with a face (docs/ia.md §8 "アイコン"): a rounded
// square whose triangular notch on the right is the mouth (the square reads as
// both C and K, the "-ck" of SockDock) and whose two vertical slots are the
// eyes. Shapes are signed distance functions in a 100×100 "face" space.
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

var (
	indigo = color.NRGBA{0x3c, 0x34, 0x89, 255}
	white  = color.NRGBA{255, 255, 255, 255}
)

// face is the signed distance (in face units, negative inside) to the white
// part of the mark in state s. TrayConnected is the plain face.
func face(x, y float64, s TrayState) float64 {
	d := roundedRect(x, y, 50, 50, 50, 50, 22)
	// Mouth: a triangle from beyond the right edge to (62, 50), its inner
	// corner rounded by 6.
	mouth := triangle(x, y, 120, 10.32, 62, 50, 120, 89.68) - 6
	d = math.Max(d, -mouth)
	var eyes float64
	if s == TrayDisconnected {
		// Closed: two short bars, set wide apart so they don't merge at 18pt.
		eyes = math.Min(roundedRect(x, y, 19, 42, 5.5, 3.5, 3.5), roundedRect(x, y, 45, 42, 5.5, 3.5, 3.5))
	} else {
		eyes = math.Min(roundedRect(x, y, 26, 38, 3.5, 10, 3.5), roundedRect(x, y, 42, 38, 3.5, 10, 3.5))
	}
	d = math.Max(d, -eyes)
	if s == TrayBusy {
		// A dot at the top-right corner, with a gap cut around it.
		d = math.Max(d, -circle(x, y, 88, 12, 25))
		d = math.Min(d, circle(x, y, 88, 12, 16))
	}
	return d
}

// TrayIcon returns a macOS template PNG (black + alpha) at 2x (36x36 px for
// an 18pt menu bar icon).
func TrayIcon(s TrayState) []byte {
	const size = 36
	const k, off = 0.3024, 2.88 // face units → px; the face fills 84% of the icon
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			u, v := (float64(px)+0.5-off)/k, (float64(py)+0.5-off)/k
			if a := coverage(face(u, v, s) * k); a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{0, 0, 0, uint8(a * 255)})
			}
		}
	}
	return encode(img)
}

// AppIcon renders the application icon at the given size: the macOS icon
// grid (an 824/1024 rounded square with a margin) in indigo, with the white
// face.
func AppIcon(size int) image.Image {
	return tile(size, 100, 185)
}

// SlackIcon renders the icon for the Slack app's settings: full bleed, since
// Slack rounds the corners itself.
func SlackIcon(size int) image.Image {
	return tile(size, 0, 0)
}

// tile draws the face on an indigo rounded square inset by margin (in a
// 1024-unit canvas) with corner radius r. The face spans 62% of the square.
func tile(size int, margin, r float64) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size) / 1024
	side := 1024 - 2*margin
	k := side * 0.62 / 100    // face units → canvas units
	off := margin + side*0.19 // where the face starts
	half := side / 2
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			x, y := (float64(px)+0.5)/s, (float64(py)+0.5)/s
			bg := coverage(roundedRect(x, y, 512, 512, half, half, r) * s)
			if bg <= 0 {
				continue
			}
			f := coverage(face((x-off)/k, (y-off)/k, TrayConnected) * k * s)
			c := mix(indigo, white, f)
			c.A = uint8(255 * bg)
			img.SetNRGBA(px, py, c)
		}
	}
	return img
}

// AppIconPNG is AppIcon encoded as PNG.
func AppIconPNG(size int) []byte { return encode(AppIcon(size)) }

// SlackIconPNG is SlackIcon encoded as PNG.
func SlackIconPNG(size int) []byte { return encode(SlackIcon(size)) }

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

// triangle is the signed distance to the triangle (ax, ay), (bx, by), (cx, cy).
func triangle(x, y, ax, ay, bx, by, cx, cy float64) float64 {
	type v struct{ x, y float64 }
	sub := func(a, b v) v { return v{a.x - b.x, a.y - b.y} }
	dot := func(a, b v) float64 { return a.x*b.x + a.y*b.y }
	cross := func(a, b v) float64 { return a.x*b.y - a.y*b.x }
	p, a, b, c := v{x, y}, v{ax, ay}, v{bx, by}, v{cx, cy}
	es := [3]v{sub(b, a), sub(c, b), sub(a, c)}
	vs := [3]v{sub(p, a), sub(p, b), sub(p, c)}
	sign := math.Copysign(1, cross(es[0], es[2]))
	dist, side := math.Inf(1), math.Inf(1)
	for i := range es {
		t := math.Max(0, math.Min(1, dot(vs[i], es[i])/dot(es[i], es[i])))
		pq := v{vs[i].x - es[i].x*t, vs[i].y - es[i].y*t}
		dist = math.Min(dist, dot(pq, pq))
		side = math.Min(side, sign*cross(vs[i], es[i]))
	}
	return -math.Sqrt(dist) * math.Copysign(1, side)
}

// coverage turns a signed distance (in pixels) into anti-aliased coverage.
func coverage(d float64) float64 { return math.Max(0, math.Min(1, 0.5-d)) }

func mix(a, b color.NRGBA, t float64) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x) + (float64(y)-float64(x))*t)) }
	return color.NRGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

func encode(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
