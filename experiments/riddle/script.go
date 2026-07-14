package main

import (
	_ "embed"
	"image"
	"log"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Tom Riddle's hand: rasterize reply text in Dancing Script, thin it to
// single-pixel pen paths (Zhang-Suen), trace them into ordered strokes, and
// yield them for stroke-by-stroke animation. Port of riddle's script.rs.

//go:embed fonts/DancingScript.ttf
var scriptTTF []byte

var (
	scriptFont *opentype.Font
	faceCache  = map[float64]font.Face{}
	faceMu     sync.Mutex
)

func InitFonts() {
	f, err := opentype.Parse(scriptTTF)
	if err != nil {
		log.Fatalf("font parse: %v", err)
	}
	scriptFont = f
}

func faceAt(px float64) font.Face {
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faceCache[px]; ok {
		return f
	}
	f, err := opentype.NewFace(scriptFont, &opentype.FaceOptions{
		Size: px, DPI: 72, Hinting: font.HintingNone,
	})
	if err != nil {
		log.Fatalf("font face %vpx: %v", px, err)
	}
	faceCache[px] = f
	return f
}

// ScriptLine is one rasterized line of text: a bit mask of inked pixels.
type ScriptLine struct {
	W, H int
	Mask []bool
}

// RasterizeLine renders one line of text at px height into a boolean mask.
func RasterizeLine(text string, px float64) *ScriptLine {
	face := faceAt(px)
	m := face.Metrics()
	w := MeasureScript(text, px) + 4
	if w < 1 {
		w = 1
	}
	h := (m.Ascent + m.Descent).Ceil() + 4
	if h < 1 {
		h = 1
	}
	img := image.NewGray(image.Rect(0, 0, w, h))
	d := font.Drawer{
		Dst:  img,
		Src:  image.White,
		Face: face,
		Dot:  fixed.P(0, m.Ascent.Ceil()),
	}
	d.DrawString(text)
	mask := make([]bool, w*h)
	for i, p := range img.Pix {
		mask[i] = p > 127 // coverage > 0.5
	}
	return &ScriptLine{W: w, H: h, Mask: mask}
}

// MeasureScript returns the advance width of text at px, in pixels.
func MeasureScript(text string, px float64) int {
	return font.MeasureString(faceAt(px), text).Ceil()
}

// Thin reduces the mask to 1px-wide skeleton lines (Zhang-Suen).
func (l *ScriptLine) Thin() {
	w, h := l.W, l.H
	idx := func(x, y int) int { return y*w + x }
	for {
		changed := false
		for phase := 0; phase < 2; phase++ {
			var toClear []int
			for y := 1; y < h-1; y++ {
				for x := 1; x < w-1; x++ {
					if !l.Mask[idx(x, y)] {
						continue
					}
					p := [8]bool{
						l.Mask[idx(x, y-1)],   // p2 N
						l.Mask[idx(x+1, y-1)], // p3 NE
						l.Mask[idx(x+1, y)],   // p4 E
						l.Mask[idx(x+1, y+1)], // p5 SE
						l.Mask[idx(x, y+1)],   // p6 S
						l.Mask[idx(x-1, y+1)], // p7 SW
						l.Mask[idx(x-1, y)],   // p8 W
						l.Mask[idx(x-1, y-1)], // p9 NW
					}
					b := 0
					for _, v := range p {
						if v {
							b++
						}
					}
					if b < 2 || b > 6 {
						continue
					}
					a := 0
					for i := 0; i < 8; i++ {
						if !p[i] && p[(i+1)%8] {
							a++
						}
					}
					if a != 1 {
						continue
					}
					var c1, c2 bool
					if phase == 0 {
						c1, c2 = !(p[0] && p[2] && p[4]), !(p[2] && p[4] && p[6])
					} else {
						c1, c2 = !(p[0] && p[2] && p[6]), !(p[0] && p[4] && p[6])
					}
					if c1 && c2 {
						toClear = append(toClear, idx(x, y))
					}
				}
			}
			if len(toClear) > 0 {
				changed = true
				for _, i := range toClear {
					l.Mask[i] = false
				}
			}
		}
		if !changed {
			break
		}
	}
}

// Pt is a screen point.
type Pt struct{ X, Y int }

// Trace walks the skeleton into polyline strokes, ordered left-to-right so
// the animation writes like a hand.
func (l *ScriptLine) Trace() [][]Pt {
	w, h := l.W, l.H
	at := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < w && y < h && l.Mask[y*w+x]
	}
	neighbors := func(x, y int) []Pt {
		var out []Pt
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if (dx != 0 || dy != 0) && at(x+dx, y+dy) {
					out = append(out, Pt{x + dx, y + dy})
				}
			}
		}
		return out
	}

	visited := make([]bool, w*h)

	// Endpoints first (degree 1), then any remaining pixels (loops).
	var starts []Pt
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if at(x, y) && len(neighbors(x, y)) == 1 {
				starts = append(starts, Pt{x, y})
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if at(x, y) {
				starts = append(starts, Pt{x, y})
			}
		}
	}

	var strokes [][]Pt
	for _, s := range starts {
		if visited[s.Y*w+s.X] {
			continue
		}
		path := []Pt{s}
		visited[s.Y*w+s.X] = true
		cx, cy := s.X, s.Y
		for {
			var next *Pt
			for _, n := range neighbors(cx, cy) {
				if !visited[n.Y*w+n.X] {
					next = &n
					break
				}
			}
			if next == nil {
				break
			}
			visited[next.Y*w+next.X] = true
			path = append(path, *next)
			cx, cy = next.X, next.Y
		}
		if len(path) >= 3 {
			strokes = append(strokes, path)
		}
	}
	// Sort by leftmost point so writing flows left to right.
	for i := 1; i < len(strokes); i++ {
		for j := i; j > 0 && minX(strokes[j]) < minX(strokes[j-1]); j-- {
			strokes[j], strokes[j-1] = strokes[j-1], strokes[j]
		}
	}
	return strokes
}

func minX(s []Pt) int {
	m := s[0].X
	for _, p := range s {
		if p.X < m {
			m = p.X
		}
	}
	return m
}

// Wrap word-wraps text to lines that fit maxPx at scale px.
func Wrap(text string, px float64, maxPx int) []string {
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		cur := ""
		for _, word := range strings.Fields(para) {
			cand := word
			if cur != "" {
				cand = cur + " " + word
			}
			if MeasureScript(cand, px) <= maxPx || cur == "" {
				cur = cand
			} else {
				lines = append(lines, cur)
				cur = word
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
	}
	return lines
}

// BlitLine paints the (unthinned) mask onto the canvas — solid text for
// panels, not the animated hand.
func (l *ScriptLine) Blit(c *image.RGBA, x, y int, g uint8) {
	for row := 0; row < l.H; row++ {
		for col := 0; col < l.W; col++ {
			if l.Mask[row*l.W+col] {
				PutPx(c, x+col, y+row, g)
			}
		}
	}
}
