package main

import "testing"

func TestPipelineProducesStrokes(t *testing.T) {
	InitFonts()
	line := RasterizeLine("Yes, Harry?", 96.0)
	if line.W <= 100 || line.H <= 50 {
		t.Fatalf("raster too small: %dx%d", line.W, line.H)
	}
	inkedBefore := 0
	for _, v := range line.Mask {
		if v {
			inkedBefore++
		}
	}
	line.Thin()
	inkedAfter := 0
	for _, v := range line.Mask {
		if v {
			inkedAfter++
		}
	}
	if inkedAfter*3 >= inkedBefore {
		t.Fatalf("thinning should slim the glyphs: %d -> %d", inkedBefore, inkedAfter)
	}
	strokes := line.Trace()
	if len(strokes) == 0 {
		t.Fatal("no strokes traced")
	}
	total := 0
	for _, s := range strokes {
		total += len(s)
	}
	t.Logf("strokes=%d total_points=%d (%dx%d)", len(strokes), total, line.W, line.H)
	if total <= 200 {
		t.Fatalf("expected a decent path length, got %d", total)
	}
	// Wrap sanity.
	lines := Wrap("Do you know anything about the Chamber of Secrets?", 96.0, 1064)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping, got %v", lines)
	}
}
