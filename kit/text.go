package kit

import (
	"image"
	"image/color"
	"log"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Legible text for reading UIs and reactive documents, in the Go font family
// (self-contained, no external files). InitFonts must be called once before
// any drawing. Sized for the ~300 ppi Libra panel (1264×1680).
//
// Note: the Go fonts have no ✎ ⌫ ⇧ glyphs — draw symbols or use ASCII.

// Face bundles a font face with its line metrics.
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
	Mono   Face // fixed-width, for values/code
	Big    Face // oversized, for a headline number
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

// InitFonts builds the default face set. Idempotent enough to call once at
// startup on both device and sim.
func InitFonts() {
	H1 = mustFace(gobold.TTF, 56)
	H2 = mustFace(gobold.TTF, 34)
	Body = mustFace(goregular.TTF, 38)
	BodyIt = mustFace(goitalic.TTF, 38)
	Bold = mustFace(gobold.TTF, 38)
	Small = mustFace(goregular.TTF, 28)
	Mono = mustFace(gomono.TTF, 34)
	Big = mustFace(gobold.TTF, 84)
}

// NewFace builds a one-off face (bold=Go Bold, else Go Regular) at a pixel size
// — for experiments that want a size the default set doesn't cover.
func NewFace(sizePx float64, bold bool) Face {
	if bold {
		return mustFace(gobold.TTF, sizePx)
	}
	return mustFace(goregular.TTF, sizePx)
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

// DrawStringTop draws with the TOP of the text at y (not the baseline).
func DrawStringTop(c *image.RGBA, f Face, s string, x, y int, g uint8) int {
	return DrawString(c, f, s, x, y+f.Asc, g)
}

func Measure(f Face, s string) int {
	return font.MeasureString(f.F, s).Ceil()
}

// Word is a laid-out word with its hit box, for selection/annotation.
type Word struct {
	S string
	R image.Rectangle // hit box (full line height)
	B int             // baseline y
}

// LayoutWords wraps text into lines of maxW starting at (x, y). Returns the
// positioned words and the y just below the last line.
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
	space := Measure(f, " ")
	for _, w := range words {
		DrawString(c, f, w.S, w.R.Min.X+space/2, w.B, g)
	}
}

// WrapDraw is layout + draw in one call; returns the end y.
func WrapDraw(c *image.RGBA, f Face, text string, x, y, maxW int, g uint8) int {
	words, endY := LayoutWords(f, text, x, y, maxW)
	DrawWords(c, f, words, g)
	return endY
}

// DrawCentered draws s horizontally centered within [x, x+w), baseline at y.
func DrawCentered(c *image.RGBA, f Face, s string, x, w, y int, g uint8) {
	tw := Measure(f, s)
	DrawString(c, f, s, x+(w-tw)/2, y, g)
}
