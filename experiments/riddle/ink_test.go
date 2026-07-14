package main

import (
	"image"
	"testing"
)

func testCanvas() *image.RGBA {
	c := image.NewRGBA(image.Rect(0, 0, 400, 400))
	FillRect(c, c.Bounds(), WHITE)
	return c
}

func totalPoints(s Strokes) int {
	n := 0
	for _, st := range s {
		n += len(st)
	}
	return n
}

func TestEraseForgetsCoveredPointsAndSplitsStrokes(t *testing.T) {
	c := testCanvas()
	ink := NewInk()
	for x := 20; x <= 200; x += 10 {
		ink.PenPoint(c, x, 100, 3)
	}
	ink.PenUp()
	if len(ink.StrokeList()) != 1 {
		t.Fatalf("strokes=%d", len(ink.StrokeList()))
	}
	before := totalPoints(ink.StrokeList())

	// Erase through the middle: the stroke splits, points vanish.
	ink.ErasePoint(c, 110, 100, 20)
	after := totalPoints(ink.StrokeList())
	if after >= before {
		t.Fatalf("erase kept every point (%d of %d)", after, before)
	}
	if len(ink.StrokeList()) != 2 {
		t.Fatalf("middle-erase should split the stroke, got %d", len(ink.StrokeList()))
	}
	for _, st := range ink.StrokeList() {
		for _, p := range st {
			if (p.X-110)*(p.X-110)+(p.Y-100)*(p.Y-100) <= 22*22 {
				t.Fatalf("surviving point under the eraser: %v", p)
			}
		}
	}
}

func TestErasingEverythingEmptiesTheInk(t *testing.T) {
	c := testCanvas()
	ink := NewInk()
	ink.PenPoint(c, 100, 100, 3)
	ink.PenPoint(c, 104, 100, 3)
	ink.PenUp()
	if ink.IsEmpty() {
		t.Fatal("ink should hold a stroke")
	}
	ink.ErasePoint(c, 102, 100, 30)
	if len(ink.StrokeList()) != 0 {
		t.Fatalf("strokes left: %d", len(ink.StrokeList()))
	}
	if !ink.BBox.IsEmpty() {
		t.Fatal("bbox should be empty")
	}
}

func TestDissolveClearsRegion(t *testing.T) {
	c := testCanvas()
	FillRect(c, image.Rect(50, 50, 150, 150), BLACK)
	region := EmptyBBox()
	region.Add(50, 50, 0)
	region.Add(149, 149, 0)
	const stages = 14
	for s := uint32(0); s < stages; s++ {
		DissolvePass(c, region, s, stages)
	}
	for y := 50; y < 150; y++ {
		for x := 50; x < 150; x++ {
			if Luma(c, x, y) != 255 {
				t.Fatalf("pixel (%d,%d) survived the dissolve", x, y)
			}
		}
	}
}
