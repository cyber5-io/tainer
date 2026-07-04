package main

// Menu-bar icon rendering. The official tainer mark (rounded brackets
// + equals, geometry from cyber5.io's tainer-mark.svg) rasterised
// with distance-field antialiasing and a soft drop shadow, at Retina
// menu-bar size. All states are rendered in-memory at startup — no
// asset files to ship or go stale.
//
// States:
//   active     — full-colour mark (pods running)
//   idle       — same mark at reduced opacity (healthy, nothing running)
//   recover[i] — spinner frames: the equals bars pulse (self-healing
//                in progress, mirrors the CLI brand spinner)
//   failed     — dimmed brackets, red ✗ core (self-healing gave up)

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// ---------------------------------------------------------------------------
// tiny alpha-aware canvas with distance-field strokes
// ---------------------------------------------------------------------------

type icanvas struct {
	w, h int
	pix  [][4]float64 // straight (non-premultiplied) RGBA, 0-255 floats
}

func newIcanvas(w, h int) *icanvas {
	return &icanvas{w: w, h: h, pix: make([][4]float64, w*h)}
}

func (c *icanvas) blend(x, y int, col color.RGBA, a float64) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h || a <= 0 {
		return
	}
	a *= float64(col.A) / 255
	p := &c.pix[y*c.w+x]
	oa := p[3] / 255
	na := a + oa*(1-a)
	if na <= 0 {
		return
	}
	p[0] = (float64(col.R)*a + p[0]*oa*(1-a)) / na
	p[1] = (float64(col.G)*a + p[1]*oa*(1-a)) / na
	p[2] = (float64(col.B)*a + p[2]*oa*(1-a)) / na
	p[3] = na * 255
}

func segDist(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = ((px-x1)*dx + (py-y1)*dy) / l2
		t = math.Max(0, math.Min(1, t))
	}
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

// stroke draws a round-capped polyline with 1px antialiased edges.
func (c *icanvas) stroke(pts [][2]float64, width float64, col color.RGBA, alpha float64) {
	r := width / 2
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range pts {
		minX, minY = math.Min(minX, p[0]), math.Min(minY, p[1])
		maxX, maxY = math.Max(maxX, p[0]), math.Max(maxY, p[1])
	}
	for y := int(minY - r - 2); y <= int(maxY+r+2); y++ {
		for x := int(minX - r - 2); x <= int(maxX+r+2); x++ {
			d := math.Inf(1)
			for i := 0; i+1 < len(pts); i++ {
				d = math.Min(d, segDist(float64(x)+0.5, float64(y)+0.5,
					pts[i][0], pts[i][1], pts[i+1][0], pts[i+1][1]))
			}
			cov := math.Min(1, r+0.5-d)
			if cov > 0 {
				c.blend(x, y, col, cov*alpha)
			}
		}
	}
}

func (c *icanvas) roundedRect(x0, y0, w, h, rad float64, col color.RGBA, alpha float64) {
	for y := int(y0) - 1; y <= int(y0+h)+1; y++ {
		for x := int(x0) - 1; x <= int(x0+w)+1; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			qx := math.Max(math.Abs(px-(x0+w/2))-(w/2-rad), 0)
			qy := math.Max(math.Abs(py-(y0+h/2))-(h/2-rad), 0)
			cov := math.Min(1, 0.5-(math.Hypot(qx, qy)-rad))
			if cov > 0 {
				c.blend(x, y, col, cov*alpha)
			}
		}
	}
}

// shadowOf returns a blurred, offset copy of c's alpha as a dark layer.
func shadowOf(c *icanvas, offY int, blur int, strength float64) *icanvas {
	s := newIcanvas(c.w, c.h)
	// seed: alpha only, shifted down
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			a := c.pix[y*c.w+x][3]
			ty := y + offY
			if a > 0 && ty >= 0 && ty < c.h {
				s.pix[ty*s.w+x] = [4]float64{0, 0, 0, a * strength}
			}
		}
	}
	// three box-blur passes ≈ gaussian
	for pass := 0; pass < 3; pass++ {
		boxBlurAlpha(s, blur)
	}
	return s
}

func boxBlurAlpha(c *icanvas, r int) {
	if r < 1 {
		return
	}
	tmp := make([]float64, c.w*c.h)
	// horizontal
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			sum, n := 0.0, 0
			for k := -r; k <= r; k++ {
				if x+k >= 0 && x+k < c.w {
					sum += c.pix[y*c.w+x+k][3]
					n++
				}
			}
			tmp[y*c.w+x] = sum / float64(n)
		}
	}
	// vertical
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			sum, n := 0.0, 0
			for k := -r; k <= r; k++ {
				if y+k >= 0 && y+k < c.h {
					sum += tmp[(y+k)*c.w+x]
					n++
				}
			}
			c.pix[y*c.w+x][3] = sum / float64(n)
		}
	}
}

func (c *icanvas) under(s *icanvas) *icanvas {
	// composite c OVER s (shadow behind mark)
	out := newIcanvas(c.w, c.h)
	copy(out.pix, s.pix)
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			p := c.pix[y*c.w+x]
			if p[3] > 0 {
				out.blend(x, y, color.RGBA{uint8(p[0]), uint8(p[1]), uint8(p[2]), 255}, p[3]/255)
			}
		}
	}
	return out
}

func (c *icanvas) encodePNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, c.w, c.h))
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			p := c.pix[y*c.w+x]
			img.SetNRGBA(x, y, color.NRGBA{uint8(p[0] + .5), uint8(p[1] + .5), uint8(p[2] + .5), uint8(p[3] + .5)})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// the mark
// ---------------------------------------------------------------------------

var (
	iconBlue   = color.RGBA{0x4E, 0x9E, 0xF4, 0xFF}
	iconOrange = color.RGBA{0xFF, 0x6B, 0x35, 0xFF}
	iconTeal   = color.RGBA{0x00, 0xD4, 0xAA, 0xFF}
	iconGrey   = color.RGBA{0x8A, 0x93, 0xA6, 0xFF}
	iconRed    = color.RGBA{0xE5, 0x48, 0x4D, 0xFF}
)

// iconSize is the @2x pixel size (menu bar renders it at half in pt).
const iconW, iconH = 44, 36

// drawMark renders the official mark geometry (viewBox 0 0 100 80,
// from tainer-mark.svg) scaled into the icon canvas. barsAlpha lets
// the spinner pulse the equals bars; markAlpha dims the whole mark.
func drawMark(c *icanvas, markAlpha, topBar, botBar float64, bracketCol1, bracketCol2, barCol color.RGBA) {
	// fit 100x80 into 44x36 with 2px margin → scale 0.4
	s := 0.4
	ox, oy := 2.0, 2.0
	// tinted panels (very light — read as depth at small size)
	panel := 0.10 * markAlpha
	c.roundedRect(ox+4*s, oy+6*s, 32*s, 68*s, 5*s, bracketCol1, panel)
	c.roundedRect(ox+64*s, oy+6*s, 32*s, 68*s, 5*s, bracketCol2, panel)
	// brackets, stroke 6.5 → 2.6px at this scale; bump to 7.5 for
	// legibility at 18pt
	bw := 7.5 * s
	c.stroke([][2]float64{{ox + 30*s, oy + 6*s}, {ox + 8*s, oy + 6*s}, {ox + 8*s, oy + 74*s}, {ox + 30*s, oy + 74*s}}, bw, bracketCol1, markAlpha)
	c.stroke([][2]float64{{ox + 70*s, oy + 6*s}, {ox + 92*s, oy + 6*s}, {ox + 92*s, oy + 74*s}, {ox + 70*s, oy + 74*s}}, bw, bracketCol2, markAlpha)
	// equals bars, stroke 7 → 8 for weight
	ew := 8.0 * s
	if topBar > 0 {
		c.stroke([][2]float64{{ox + 32*s, oy + 30*s}, {ox + 68*s, oy + 30*s}}, ew, barCol, markAlpha*topBar)
	}
	if botBar > 0 {
		c.stroke([][2]float64{{ox + 32*s, oy + 50*s}, {ox + 68*s, oy + 50*s}}, ew, barCol, markAlpha*botBar)
	}
}

func renderIcon(markAlpha, topBar, botBar float64, b1, b2, bar color.RGBA, redX bool) []byte {
	c := newIcanvas(iconW, iconH)
	drawMark(c, markAlpha, topBar, botBar, b1, b2, bar)
	if redX {
		// red ✗ between the brackets
		s := 0.4
		ox, oy := 2.0, 2.0
		c.stroke([][2]float64{{ox + 36*s, oy + 26*s}, {ox + 64*s, oy + 54*s}}, 8*s, iconRed, 1)
		c.stroke([][2]float64{{ox + 64*s, oy + 26*s}, {ox + 36*s, oy + 54*s}}, 8*s, iconRed, 1)
	}
	sh := shadowOf(c, 1, 1, 0.35)
	return c.under(sh).encodePNG()
}

// iconStates builds every state the menu app needs.
type iconSet struct {
	active   []byte
	idle     []byte
	failed   []byte
	recover_ [][]byte // spinner frames
}

func buildIcons() iconSet {
	var set iconSet
	set.active = renderIcon(1.0, 1, 1, iconBlue, iconOrange, iconTeal, false)
	set.idle = renderIcon(0.45, 1, 1, iconBlue, iconOrange, iconTeal, false)
	set.failed = renderIcon(0.55, 0, 0, iconGrey, iconGrey, iconGrey, true)
	// spinner: bars pulse alternately (top fades in as bottom fades out)
	steps := []struct{ top, bot float64 }{
		{1.0, 0.25}, {0.7, 0.55}, {0.4, 0.85}, {0.25, 1.0}, {0.55, 0.7}, {0.85, 0.4},
	}
	for _, st := range steps {
		set.recover_ = append(set.recover_, renderIcon(0.6, st.top, st.bot, iconBlue, iconOrange, iconTeal, false))
	}
	return set
}
