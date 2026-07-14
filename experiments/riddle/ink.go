package main

import (
	"errors"
	"image"
	"image/png"
	"os"
)

// User ink: capture pen strokes, render them, dissolve them, rasterize them
// for the oracle. Port of riddle's ink.rs.

// InkPt is one pen sample: position and brush radius.
type InkPt struct{ X, Y, R int }

// Strokes is a page of ink: finished strokes as point lists.
type Strokes = [][]InkPt

type Ink struct {
	strokes   Strokes
	current   []InkPt
	lastErase *Pt
	BBox      BBox
}

func NewInk() *Ink {
	return &Ink{BBox: EmptyBBox()}
}

func (k *Ink) IsEmpty() bool { return len(k.strokes) == 0 && len(k.current) == 0 }

// StrokeList returns finished strokes (the in-flight stroke is not included).
func (k *Ink) StrokeList() Strokes { return k.strokes }

func (k *Ink) Clear() {
	k.strokes = nil
	k.current = nil
	k.lastErase = nil
	k.BBox = EmptyBBox()
}

// PenPoint: pen touched down or moved while down, with brush radius already
// resolved by the caller. Returns the dirty box of what was drawn.
func (k *Ink) PenPoint(c *image.RGBA, x, y, r int) BBox {
	dirty := EmptyBBox()
	if n := len(k.current); n > 0 {
		p := k.current[n-1]
		BrushLine(c, p.X, p.Y, x, y, min(r, p.R+1), BLACK)
		dirty.Add(p.X, p.Y, p.R+2)
	} else {
		Stamp(c, x, y, r, BLACK)
	}
	dirty.Add(x, y, r+2)
	k.current = append(k.current, InkPt{x, y, r})
	k.BBox.Add(x, y, r+2)
	return dirty
}

// ErasePoint: brush white over the page AND drop the stored points it covers,
// so the stroke model stays true to the visible page.
func (k *Ink) ErasePoint(c *image.RGBA, x, y, r int) BBox {
	dirty := EmptyBBox()
	if k.lastErase != nil {
		BrushLine(c, k.lastErase.X, k.lastErase.Y, x, y, r, WHITE)
		dirty.Add(k.lastErase.X, k.lastErase.Y, r+2)
	} else {
		Stamp(c, x, y, r, WHITE)
	}
	dirty.Add(x, y, r+2)
	k.forgetNear(x, y, r)
	k.lastErase = &Pt{x, y}
	return dirty
}

// forgetNear removes committed stroke points within r of (x, y); splits
// strokes erased through the middle, and recomputes the ink bbox.
func (k *Ink) forgetNear(x, y, r int) {
	r2 := (r + 2) * (r + 2)
	var kept Strokes
	for _, stroke := range k.strokes {
		var seg []InkPt
		for _, p := range stroke {
			dx, dy := p.X-x, p.Y-y
			if dx*dx+dy*dy <= r2 {
				if len(seg) > 0 {
					kept = append(kept, seg)
					seg = nil
				}
			} else {
				seg = append(seg, p)
			}
		}
		if len(seg) > 0 {
			kept = append(kept, seg)
		}
	}
	k.strokes = kept
	k.BBox = EmptyBBox()
	for _, stroke := range k.strokes {
		for _, p := range stroke {
			k.BBox.Add(p.X, p.Y, p.R+2)
		}
	}
}

func (k *Ink) PenUp() {
	if len(k.current) > 0 {
		k.strokes = append(k.strokes, k.current)
		k.current = nil
	}
	k.lastErase = nil
}

// ToPNG rasterizes the ink region to a grayscale PNG for the oracle.
// Crops to the ink bounding box and box-downscales so the long side stays
// ≤ 800px (at least 2x): the model reads handwriting fine at that scale,
// and image pixels are the dominant vision-token / latency cost.
func (k *Ink) ToPNG(c *image.RGBA, path string) error {
	if k.BBox.IsEmpty() {
		return errors.New("no ink")
	}
	b := c.Bounds()
	x0 := max(k.BBox.X0-20, 0)
	y0 := max(k.BBox.Y0-20, 0)
	x1 := min(k.BBox.X1+1+20, b.Max.X)
	y1 := min(k.BBox.Y1+1+20, b.Max.Y)
	long := max(x1-x0, y1-y0)
	f := max((long+799)/800, 2)
	w, h := (x1-x0)/f, (y1-y0)/f

	gray := image.NewGray(image.Rect(0, 0, w, h))
	for oy := 0; oy < h; oy++ {
		for ox := 0; ox < w; ox++ {
			acc := 0
			for sy := 0; sy < f; sy++ {
				for sx := 0; sx < f; sx++ {
					acc += int(Luma(c, x0+ox*f+sx, y0+oy*f+sy))
				}
			}
			gray.Pix[oy*gray.Stride+ox] = uint8(acc / (f * f))
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(file, gray)
}

// pxHash is a deterministic per-pixel hash for the dissolve pattern.
func pxHash(x, y int) uint32 {
	h := uint32(x)*0x9E3779B1 ^ uint32(y)*0x85EBCA6B
	h ^= h >> 13
	h *= 0xC2B2AE35
	return h ^ (h >> 16)
}

// DissolvePass is one pass of the "diary drinks the ink" effect: erase the
// pixels whose hash falls in this stage. After `stages` passes the region is
// clean white.
func DissolvePass(c *image.RGBA, region BBox, stage, stages uint32) {
	if region.IsEmpty() {
		return
	}
	for y := region.Y0; y <= region.Y1; y++ {
		for x := region.X0; x <= region.X1; x++ {
			if Luma(c, x, y) < 250 && pxHash(x, y)%stages <= stage {
				PutPx(c, x, y, WHITE)
			}
		}
	}
}
