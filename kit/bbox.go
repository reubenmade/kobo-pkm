package kit

import "image"

// BBox is a growable dirty-region rectangle (inclusive coordinates). Refresh
// exactly the region you changed and nothing more — that discipline is what
// keeps e-ink interaction feeling instant.
type BBox struct {
	X0, Y0, X1, Y1 int
}

func EmptyBBox() BBox {
	return BBox{X0: 1 << 30, Y0: 1 << 30, X1: -(1 << 30), Y1: -(1 << 30)}
}

func (b *BBox) IsEmpty() bool { return b.X1 < b.X0 || b.Y1 < b.Y0 }

// Add grows the box to include (x±pad, y±pad).
func (b *BBox) Add(x, y, pad int) {
	if x-pad < b.X0 {
		b.X0 = x - pad
	}
	if y-pad < b.Y0 {
		b.Y0 = y - pad
	}
	if x+pad > b.X1 {
		b.X1 = x + pad
	}
	if y+pad > b.Y1 {
		b.Y1 = y + pad
	}
}

// AddRect grows the box to include an image.Rectangle.
func (b *BBox) AddRect(r image.Rectangle) {
	if r.Empty() {
		return
	}
	b.Add(r.Min.X, r.Min.Y, 0)
	b.Add(r.Max.X-1, r.Max.Y-1, 0)
}

// Union grows the box to include another box.
func (b *BBox) Union(o BBox) {
	if o.IsEmpty() {
		return
	}
	b.Add(o.X0, o.Y0, 0)
	b.Add(o.X1, o.Y1, 0)
}

// Rect returns the box as an image.Rectangle (exclusive max), clamped to clip.
func (b BBox) Rect(clip image.Rectangle) image.Rectangle {
	if b.IsEmpty() {
		return image.Rectangle{}
	}
	return image.Rect(b.X0, b.Y0, b.X1+1, b.Y1+1).Intersect(clip)
}
