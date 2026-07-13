package main

import (
	"image"
	"image/color"
	"log"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Face bundles a font face with its line height in pixels.
type Face struct {
	F    font.Face
	Line int // baseline-to-baseline
	Asc  int // ascent above baseline
}

var (
	H1     Face // screen titles
	H2     Face // section headers
	Body   Face
	BodyIt Face
	Bold   Face
	Small  Face
	Key    Face // on-screen keyboard caps
)

func mustFace(ttf []byte, sizePx float64) Face {
	f, err := opentype.Parse(ttf)
	if err != nil {
		log.Fatalf("font parse: %v", err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size: sizePx, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		log.Fatalf("font face: %v", err)
	}
	m := face.Metrics()
	return Face{
		F:    face,
		Line: (m.Height + m.Height/6).Ceil(),
		Asc:  m.Ascent.Ceil(),
	}
}

func InitFonts() {
	// Sized for a 300ppi panel (Libra 2: 1264x1680).
	H1 = mustFace(gobold.TTF, 56)
	H2 = mustFace(gobold.TTF, 30)
	Body = mustFace(goregular.TTF, 34)
	BodyIt = mustFace(goitalic.TTF, 34)
	Bold = mustFace(gobold.TTF, 34)
	Small = mustFace(goregular.TTF, 26)
	Key = mustFace(gobold.TTF, 38)
}

// DrawString draws s with its baseline at y, returns the advance in pixels.
func DrawString(c *image.RGBA, f Face, s string, x, y int, g uint8) int {
	d := font.Drawer{
		Dst:  c,
		Src:  image.NewUniform(color.Gray{Y: g}),
		Face: f.F,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
	return (d.Dot.X - fixed.I(x)).Ceil()
}

func Measure(f Face, s string) int {
	return font.MeasureString(f.F, s).Ceil()
}

// DrawStringTop draws with the TOP of the text at y (not the baseline).
func DrawStringTop(c *image.RGBA, f Face, s string, x, y int, g uint8) int {
	return DrawString(c, f, s, x, y+f.Asc, g)
}

// Word is a laid-out word with its hit box, used for highlight selection.
type Word struct {
	S string
	R image.Rectangle // hit box (full line height)
	B int             // baseline y
}

// LayoutWords wraps text into lines of maxW starting at (x, y).
// Returns the positioned words and the y just below the last line.
func LayoutWords(f Face, text string, x, y, maxW int) ([]Word, int) {
	var out []Word
	space := Measure(f, " ")
	cx, cy := x, y
	for _, w := range strings.Fields(text) {
		ww := Measure(f, w)
		if cx+ww > x+maxW && cx > x {
			cx = x
			cy += f.Line
		}
		out = append(out, Word{
			S: w,
			R: image.Rect(cx-space/2, cy, cx+ww+space/2, cy+f.Line),
			B: cy + f.Asc,
		})
		cx += ww + space
	}
	if len(out) > 0 {
		cy += f.Line
	}
	return out, cy
}

// DrawWords renders previously laid-out words.
func DrawWords(c *image.RGBA, f Face, words []Word, g uint8) {
	for _, w := range words {
		space := Measure(f, " ")
		DrawString(c, f, w.S, w.R.Min.X+space/2, w.B, g)
	}
}

// WrapDraw is a convenience: layout + draw, returns end y.
func WrapDraw(c *image.RGBA, f Face, text string, x, y, maxW int, g uint8) int {
	words, endY := LayoutWords(f, text, x, y, maxW)
	DrawWords(c, f, words, g)
	return endY
}
