package main

import "image"

// Grayscale drawing primitives on the RGBA canvas. Ink is monochrome:
// 0 = black, 255 = white; FADED is how the diary writes its memories.
const (
	BLACK uint8 = 0
	WHITE uint8 = 255
	FADED uint8 = 122
)

func PutPx(c *image.RGBA, x, y int, g uint8) {
	if !(image.Pt(x, y).In(c.Bounds())) {
		return
	}
	i := c.PixOffset(x, y)
	c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = g, g, g, 255
}

// Luma returns 0..255 brightness at (x, y); off-canvas reads as white.
func Luma(c *image.RGBA, x, y int) uint8 {
	if !(image.Pt(x, y).In(c.Bounds())) {
		return 255
	}
	// Green approximates luma well enough for mono ink.
	return c.Pix[c.PixOffset(x, y)+1]
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

// Stamp draws a filled circle of radius r — one pen dab.
func Stamp(c *image.RGBA, cx, cy, r int, g uint8) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy <= r*r {
				PutPx(c, cx+dx, cy+dy, g)
			}
		}
	}
}

// BrushLine stamps along the segment — how both the writer's ink and Tom's
// hand are stroked.
func BrushLine(c *image.RGBA, x0, y0, x1, y1, r int, g uint8) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	steps := max(max(dx, dy), 1)
	for i := 0; i <= steps; i++ {
		Stamp(c, x0+(x1-x0)*i/steps, y0+(y1-y0)*i/steps, r, g)
	}
}

// CopyRect snapshots a rect's raw canvas bytes (for save-under panels).
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

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
