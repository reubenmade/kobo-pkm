package main

import (
	"image"
	"image/color"
)

// Drawing primitives on an RGBA canvas (the Libra Colour has a Kaleido 3
// colour panel). Grayscale helpers take g: 0 = black, 255 = white.

func FillRGB(c *image.RGBA, r image.Rectangle, col color.RGBA) {
	r = r.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := c.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			c.Pix[i] = col.R
			c.Pix[i+1] = col.G
			c.Pix[i+2] = col.B
			c.Pix[i+3] = 255
			i += 4
		}
	}
}

func FillRect(c *image.RGBA, r image.Rectangle, g uint8) {
	FillRGB(c, r, color.RGBA{g, g, g, 255})
}

func StrokeRect(c *image.RGBA, r image.Rectangle, w int, g uint8) {
	FillRect(c, image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+w), g)
	FillRect(c, image.Rect(r.Min.X, r.Max.Y-w, r.Max.X, r.Max.Y), g)
	FillRect(c, image.Rect(r.Min.X, r.Min.Y, r.Min.X+w, r.Max.Y), g)
	FillRect(c, image.Rect(r.Max.X-w, r.Min.Y, r.Max.X, r.Max.Y), g)
}

func InvertRect(c *image.RGBA, r image.Rectangle) {
	r = r.Intersect(c.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		i := c.PixOffset(r.Min.X, y)
		for x := r.Min.X; x < r.Max.X; x++ {
			c.Pix[i] = 255 - c.Pix[i]
			c.Pix[i+1] = 255 - c.Pix[i+1]
			c.Pix[i+2] = 255 - c.Pix[i+2]
			i += 4
		}
	}
}

func HLine(c *image.RGBA, x0, x1, y, w int, g uint8) {
	FillRect(c, image.Rect(x0, y, x1, y+w), g)
}

// Line draws a thick line segment (used for pen strokes).
func Line(c *image.RGBA, x0, y0, x1, y1, w int, g uint8) {
	dx := abs(x1 - x0)
	dy := abs(y1 - y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx - dy
	for {
		FillRect(c, image.Rect(x0-w/2, y0-w/2, x0+w/2+1, y0+w/2+1), g)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x0 += sx
		}
		if e2 < dx {
			err += dx
			y0 += sy
		}
	}
}

func CircleRGB(c *image.RGBA, cx, cy, r, w int, col color.RGBA) {
	set := func(px, py int) {
		if image.Pt(px, py).In(c.Bounds()) {
			i := c.PixOffset(px, py)
			c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = col.R, col.G, col.B, 255
		}
	}
	for t := -w / 2; t <= w/2; t++ {
		rr := r + t
		x, y, err := rr, 0, 0
		for x >= y {
			for _, p := range [][2]int{{x, y}, {y, x}, {-y, x}, {-x, y}, {-x, -y}, {-y, -x}, {y, -x}, {x, -y}} {
				set(cx+p[0], cy+p[1])
			}
			y++
			err += 1 + 2*y
			if 2*(err-x)+1 > 0 {
				x--
				err += 1 - 2*x
			}
		}
	}
}

func Circle(c *image.RGBA, cx, cy, r, w int, g uint8) {
	CircleRGB(c, cx, cy, r, w, color.RGBA{g, g, g, 255})
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
