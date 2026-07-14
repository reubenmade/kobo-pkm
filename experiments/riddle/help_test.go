package main

import (
	"image"
	"math"
	"testing"
)

func mkStroke(pts []Pt) []InkPt {
	out := make([]InkPt, len(pts))
	for i, p := range pts {
		out[i] = InkPt{p.X, p.Y, 3}
	}
	return out
}

// Parametric "?": hook (arc sweeping over the top and curling back) then a
// straight descender; optional dot. From riddle's help.rs tests.
func testQuestionMark(scale float64, withDot, reversed bool) Strokes {
	var pts []Pt
	cx, cy, r := 200*scale, 180*scale, 120*scale
	for deg := 180.0; deg <= 450.0; deg += 6 {
		a := deg * math.Pi / 180
		pts = append(pts, Pt{int(cx + r*math.Cos(a)), int(cy + r*math.Sin(a))})
	}
	dx, dy := int(cx), int(cy+r)
	for i := 1; i <= 20; i++ {
		pts = append(pts, Pt{dx, dy + int(float64(i)*13*scale)})
	}
	if reversed {
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
	}
	out := Strokes{mkStroke(pts)}
	if withDot {
		ddy := dy + int(300*scale) + 60
		out = append(out, mkStroke([]Pt{{dx - 5, ddy}, {dx + 5, ddy + 5}, {dx, ddy + 8}}))
	}
	return out
}

func TestDetectsQuestionMarks(t *testing.T) {
	for _, c := range []struct {
		scale         float64
		dot, reversed bool
	}{{1.5, true, false}, {1.5, false, false}, {1.5, true, true}, {3.0, true, false}} {
		if !LooksLikeQuestionMark(testQuestionMark(c.scale, c.dot, c.reversed)) {
			t.Fatalf("missed ? at scale=%v dot=%v rev=%v", c.scale, c.dot, c.reversed)
		}
	}
}

func TestRejectsNonQuestionMarks(t *testing.T) {
	// Too small (normal end-of-sentence "?").
	if LooksLikeQuestionMark(testQuestionMark(0.5, true, false)) {
		t.Fatal("accepted a small ?")
	}
	// "!" — vertical bar plus dot.
	var bar []Pt
	for i := 0; i < 40; i++ {
		bar = append(bar, Pt{200, 60 + i*12})
	}
	if LooksLikeQuestionMark(Strokes{mkStroke(bar), mkStroke([]Pt{{200, 600}, {204, 604}})}) {
		t.Fatal("accepted a !")
	}
	// "7" — flat top bar, diagonal descender.
	var seven []Pt
	for i := 0; i < 20; i++ {
		seven = append(seven, Pt{80 + i*12, 60})
	}
	for i := 0; i < 40; i++ {
		seven = append(seven, Pt{320 - i*4, 60 + i*12})
	}
	if LooksLikeQuestionMark(Strokes{mkStroke(seven)}) {
		t.Fatal("accepted a 7")
	}
	// Two long strokes side by side (writing, not a glyph).
	var l1, l2 []Pt
	for i := 0; i < 40; i++ {
		l1 = append(l1, Pt{100, 60 + i*10})
		l2 = append(l2, Pt{400, 60 + i*10})
	}
	if LooksLikeQuestionMark(Strokes{mkStroke(l1), mkStroke(l2)}) {
		t.Fatal("accepted two bars")
	}
	// Empty / too many strokes.
	if LooksLikeQuestionMark(nil) {
		t.Fatal("accepted empty")
	}
	dot := mkStroke([]Pt{{0, 0}, {1, 1}})
	if LooksLikeQuestionMark(Strokes{dot, dot, dot, dot}) {
		t.Fatal("accepted four dots")
	}
}

func TestModalRendersAndRestores(t *testing.T) {
	InitFonts()
	c := image.NewRGBA(image.Rect(0, 0, 1264, 1680))
	FillRect(c, c.Bounds(), WHITE)
	// Scribble something under the panel area so restore is observable.
	FillRect(c, image.Rect(700, 1000, 900, 1200), BLACK)
	before := CopyRect(c, c.Bounds())

	panel := ShowHelp(c)
	r := panel.Region.Rect(c.Bounds())
	if r.Dx() < 400 || r.Dy() < 400 {
		t.Fatalf("panel too small: %v", r)
	}
	black := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if Luma(c, x, y) < 128 {
				black++
			}
		}
	}
	if black < 5000 {
		t.Fatalf("panel looks empty: %d dark px", black)
	}

	panel.Dismiss(c)
	after := CopyRect(c, c.Bounds())
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("restore is not exact")
		}
	}
}

func TestSleepPageRendersAndRestores(t *testing.T) {
	InitFonts()
	c := image.NewRGBA(image.Rect(0, 0, 1264, 1680))
	FillRect(c, c.Bounds(), WHITE)
	FillRect(c, image.Rect(300, 300, 700, 700), BLACK)
	before := CopyRect(c, c.Bounds())

	saved := ShowSleep(c)
	black := 0
	b := c.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if Luma(c, x, y) < 128 {
				black++
			}
		}
	}
	if black < 10000 {
		t.Fatalf("sleep page looks empty: %d dark px", black)
	}

	RestoreSleep(c, saved)
	after := CopyRect(c, c.Bounds())
	for i := range before {
		if before[i] != after[i] {
			t.Fatal("sleep restore is not exact")
		}
	}
}
