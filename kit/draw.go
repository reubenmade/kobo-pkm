package kit

import "image"

// Mono drawing primitives on the RGBA canvas. Ink is grayscale: 0 = black,
// 255 = white. The Kaleido panel is colour-capable, but reading UIs are mono;
// use RGBA directly for colour figures.
const (
	BLACK uint8 = 0
	WHITE uint8 = 255
	GRAY  uint8 = 122
	LGRAY uint8 = 200
)

func PutPx(c *image.RGBA, x, y int, g uint8) {
	if !(image.Pt(x, y).In(c.Bounds())) {
		return
	}
	i := c.PixOffset(x, y)
	c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = g, g, g, 255
}

// PutRGB sets a colour pixel (for figures on the Kaleido panel).
func PutRGB(c *image.RGBA, x, y int, r, g, b uint8) {
	if !(image.Pt(x, y).In(c.Bounds())) {
		return
	}
	i := c.PixOffset(x, y)
	c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = r, g, b, 255
}

// Luma returns 0..255 brightness at (x, y); off-canvas reads as white.
func Luma(c *image.RGBA, x, y int) uint8 {
	if !(image.Pt(x, y).In(c.Bounds())) {
		return 255
	}
	return c.Pix[c.PixOffset(x, y)+1] // green approximates luma for mono ink
}

func FillRect(c *image.RGBA, r image.Rectangle, g uint8) {
	r = r.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := c.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = g, g, g, 255
			i += 4
		}
	}
}

// Frame draws a rectangular outline of thickness t.
func Frame(c *image.RGBA, x, y, w, h, t int, g uint8) {
	FillRect(c, image.Rect(x, y, x+w, y+t), g)
	FillRect(c, image.Rect(x, y+h-t, x+w, y+h), g)
	FillRect(c, image.Rect(x, y, x+t, y+h), g)
	FillRect(c, image.Rect(x+w-t, y, x+w, y+h), g)
}

// Stamp draws a filled disc of radius r — one pen dab.
func Stamp(c *image.RGBA, cx, cy, r int, g uint8) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				PutPx(c, cx+dx, cy+dy, g)
			}
		}
	}
}

// BrushLine stamps discs along the segment — how pen ink and traced hands are
// stroked.
func BrushLine(c *image.RGBA, x0, y0, x1, y1, r int, g uint8) {
	dx := iabs(x1 - x0)
	dy := iabs(y1 - y0)
	steps := imax(imax(dx, dy), 1)
	for i := 0; i <= steps; i++ {
		Stamp(c, x0+(x1-x0)*i/steps, y0+(y1-y0)*i/steps, r, g)
	}
}

// Line draws a 1px Bresenham line (for plots and rules).
func Line(c *image.RGBA, x0, y0, x1, y1 int, g uint8) {
	dx := iabs(x1 - x0)
	dy := -iabs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		PutPx(c, x0, y0, g)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

// CopyRect snapshots a rect's raw canvas bytes (for save-under panels and the
// suspend save/restore).
func CopyRect(c *image.RGBA, r image.Rectangle) []byte {
	r = r.Intersect(c.Bounds())
	out := make([]byte, 0, r.Dx()*r.Dy()*4)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		s := c.PixOffset(r.Min.X, y)
		out = append(out, c.Pix[s:s+r.Dx()*4]...)
	}
	return out
}

// PasteRect puts back bytes captured by CopyRect with the same geometry.
func PasteRect(c *image.RGBA, r image.Rectangle, data []byte) {
	r = r.Intersect(c.Bounds())
	rowLen := r.Dx() * 4
	for i, y := 0, r.Min.Y; y < r.Max.Y; i, y = i+1, y+1 {
		s := c.PixOffset(r.Min.X, y)
		copy(c.Pix[s:s+rowLen], data[i*rowLen:(i+1)*rowLen])
	}
}

// ---- small int helpers (kept unexported/prefixed so they don't collide with
// an experiment's own min/max) ----

func iabs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func imin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Clamp constrains v to [lo, hi].
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
