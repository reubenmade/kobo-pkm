package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"strings"
)

// pageWord is a laid-out word carrying its global index in the article,
// so highlight ranges survive re-pagination on other screen sizes.
type pageWord struct {
	Word
	Idx int
}

type figPlace struct {
	R     image.Rectangle
	Style int
}

type page struct {
	words []pageWord
	figs  []figPlace
}

type Article struct {
	def    ArticleDef
	pages  []page
	cur    int
	btns   []*Button
	m      int // margin
	textW  int
	top    int // content area top
	bottom int // content area bottom

	// active selection (word index range, -1 = none)
	selAnchor int
	selCur    int
	dragging  bool
	downAt    image.Point
}

const figHeight = 420

func NewArticle(a *App, def ArticleDef) *Article {
	s := &Article{def: def, m: 70, top: 150, selAnchor: -1, selCur: -1}
	s.bottom = a.H - 130
	s.textW = a.W - 2*s.m
	s.paginate(a)
	return s
}

// paginate flows paragraphs (and figures) into pages, keeping global word
// indexes for highlight addressing.
func (s *Article) paginate(a *App) {
	s.pages = nil
	cur := page{}
	y := s.top
	gw := 0 // global word counter
	newPage := func() {
		s.pages = append(s.pages, cur)
		cur = page{}
		y = s.top
	}
	for pi, para := range s.def.Paras {
		words, endY := LayoutWords(Body, para, s.m, y, s.textW)
		if endY > s.bottom && len(cur.words)+len(cur.figs) > 0 {
			newPage()
			words, endY = LayoutWords(Body, para, s.m, y, s.textW)
		}
		// If a single paragraph still overflows, split it word by word.
		if endY > s.bottom {
			fields := strings.Fields(para)
			taken := 0
			for taken < len(fields) {
				words, endY = LayoutWords(Body, strings.Join(fields[taken:], " "), s.m, y, s.textW)
				n := 0
				for _, w := range words {
					if w.R.Max.Y > s.bottom {
						break
					}
					n++
				}
				if n == 0 {
					newPage()
					continue
				}
				for i := 0; i < n; i++ {
					cur.words = append(cur.words, pageWord{words[i], gw})
					gw++
				}
				taken += n
				if taken < len(fields) {
					newPage()
				} else {
					y = words[n-1].R.Max.Y + Body.Line/2
				}
			}
		} else {
			for _, w := range words {
				cur.words = append(cur.words, pageWord{w, gw})
				gw++
			}
			y = endY + Body.Line/2
		}
		if style, ok := s.def.FigAfter[pi]; ok {
			if y+figHeight > s.bottom {
				newPage()
			}
			cur.figs = append(cur.figs, figPlace{image.Rect(s.m, y, a.W-s.m, y+figHeight), style})
			y += figHeight + Body.Line
		}
	}
	if len(cur.words)+len(cur.figs) > 0 {
		s.pages = append(s.pages, cur)
	}
}

func (s *Article) Render(a *App) {
	c := a.D.Canvas()
	// header
	DrawStatusBar(a)
	DrawStringTop(c, H2, s.def.Title, s.m, StatusBarH+14, 0)
	DrawStringTop(c, Small, fmt.Sprintf("page %d / %d", s.cur+1, len(s.pages)), a.W-s.m-200, StatusBarH+20, 60)
	HLine(c, s.m, a.W-s.m, StatusBarH+H2.Line+28, 3, 0)

	p := s.pages[s.cur]
	// figures first (words never overlap them)
	for _, f := range p.figs {
		drawFigure(c, f.R, f.Style)
	}
	// words + saved highlights: colour highlights fill the text background,
	// noted segments get an underline instead.
	hls := a.Store.ForArticle(s.def.ID)
	for _, w := range p.words {
		var bg *color.RGBA
		underline := false
		for _, h := range hls {
			if w.Idx >= h.Start && w.Idx <= h.End {
				if h.Color != "" {
					cc := hlColorByName(h.Color)
					bg = &cc
				}
				// underline marks a note ONLY when there's no colour fill
				if h.Note != nil && h.Color == "" {
					underline = true
				}
			}
		}
		if bg != nil {
			FillRGB(c, w.R, *bg)
		}
		space := Measure(Body, " ")
		DrawString(c, Body, w.S, w.R.Min.X+space/2, w.B, 0)
		if underline {
			HLine(c, w.R.Min.X, w.R.Max.X, w.B+8, 5, 0)
		}
	}
	// active selection inversion
	if s.selAnchor >= 0 {
		lo, hi := s.selRange()
		for _, w := range p.words {
			if w.Idx >= lo && w.Idx <= hi {
				InvertRect(c, w.R)
			}
		}
	}

	// footer
	HLine(c, s.m, a.W-s.m, s.bottom+20, 3, 0)
	s.btns = []*Button{{
		R: image.Rect(s.m, a.H-100, s.m+280, a.H-25), Label: "back",
		OnTap: func() { a.Pop() },
	}}
	if s.cur > 0 {
		s.btns = append(s.btns, &Button{
			R: image.Rect(a.W-s.m-330, a.H-100, a.W-s.m-180, a.H-25), Label: "<",
			OnTap: func() { s.cur--; a.Render(RefreshFull) },
		})
	}
	if s.cur < len(s.pages)-1 {
		s.btns = append(s.btns, &Button{
			R: image.Rect(a.W-s.m-150, a.H-100, a.W-s.m, a.H-25), Label: ">", Bold: true,
			OnTap: func() { s.cur++; a.Render(RefreshFull) },
		})
	}
	DrawStringTop(c, Small, "drag words to highlight", s.m+310, a.H-80, 100)
	for _, b := range s.btns {
		b.Draw(c)
	}
}

// BtnSwap flips the physical page-button direction (config: swapbtn).
var BtnSwap bool

// Key handles the physical page-turn buttons: F23=193 back, F24=194 forward
// on the Libra family (KEY_PAGEUP/PAGEDOWN kept for other models).
func (s *Article) Key(a *App, code int) {
	prev := code == 193 || code == 104
	next := code == 194 || code == 109
	if BtnSwap {
		prev, next = next, prev
	}
	switch {
	case prev && s.cur > 0:
		s.cur--
		a.Render(RefreshFull)
	case next && s.cur < len(s.pages)-1:
		s.cur++
		a.Render(RefreshFull)
	}
}

func (s *Article) selRange() (int, int) {
	lo, hi := s.selAnchor, s.selCur
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

func (s *Article) wordAt(pt image.Point) *pageWord {
	p := s.pages[s.cur]
	for i := range p.words {
		if pt.In(p.words[i].R) {
			return &p.words[i]
		}
	}
	return nil
}

func (s *Article) Touch(a *App, t Touch) {
	pt := image.Pt(t.X, t.Y)
	switch t.Kind {
	case TouchDown:
		s.downAt = pt
		s.dragging = false
		if w := s.wordAt(pt); w != nil {
			s.selAnchor = w.Idx
			s.selCur = w.Idx
		} else {
			s.selAnchor = -1
		}

	case TouchMove:
		if s.selAnchor < 0 {
			return
		}
		d := pt.Sub(s.downAt)
		if !s.dragging && d.X*d.X+d.Y*d.Y < 30*30 {
			return // not a drag yet
		}
		if !s.dragging {
			// Only start selecting once the drag reaches a *different* word:
			// stylus taps skitter a few px and must still read as taps.
			w := s.wordAt(pt)
			if w == nil || w.Idx == s.selAnchor {
				return
			}
			s.dragging = true
			s.invertSel(a, s.selAnchor, s.selAnchor, false)
		}
		if w := s.wordAt(pt); w != nil && w.Idx != s.selCur {
			old := s.selCur
			s.selCur = w.Idx
			s.updateSelDelta(a, old)
		}

	case TouchUp:
		if s.dragging && s.selAnchor >= 0 {
			s.dragging = false
			lo, hi := s.selRange()
			s.showSelectionActions(a, lo, hi)
			return
		}
		s.selAnchor = -1
		if hitButtons(a, t, s.btns) {
			return
		}
		// tap on an existing highlight -> view note
		if w := s.wordAt(pt); w != nil {
			for _, h := range a.Store.ForArticle(s.def.ID) {
				if w.Idx >= h.Start && w.Idx <= h.End {
					s.openNoteEditor(a, h)
					return
				}
			}
		}
		// edge taps: page turn
		if t.X > a.W*3/4 && s.cur < len(s.pages)-1 {
			s.cur++
			a.Render(RefreshFull)
		} else if t.X < a.W/4 && s.cur > 0 {
			s.cur--
			a.Render(RefreshFull)
		}
	}
}

// invertSel inverts words in [lo,hi] on the canvas and refreshes them (DU).
func (s *Article) invertSel(a *App, lo, hi int, _ bool) {
	var union image.Rectangle
	for _, w := range s.pages[s.cur].words {
		if w.Idx >= lo && w.Idx <= hi {
			InvertRect(a.D.Canvas(), w.R)
			union = union.Union(w.R)
		}
	}
	if !union.Empty() {
		a.D.Refresh(union, RefreshFast)
	}
}

// updateSelDelta flips only the words whose selection state changed.
func (s *Article) updateSelDelta(a *App, oldCur int) {
	lo, hi := s.selRange()
	oldLo, oldHi := s.selAnchor, oldCur
	if oldLo > oldHi {
		oldLo, oldHi = oldHi, oldLo
	}
	var union image.Rectangle
	for _, w := range s.pages[s.cur].words {
		was := w.Idx >= oldLo && w.Idx <= oldHi
		is := w.Idx >= lo && w.Idx <= hi
		if was != is {
			InvertRect(a.D.Canvas(), w.R)
			union = union.Union(w.R)
		}
	}
	if !union.Empty() {
		a.D.Refresh(union, RefreshFast)
	}
}

func (s *Article) selectedText(lo, hi int) string {
	var parts []string
	for _, p := range s.pages {
		for _, w := range p.words {
			if w.Idx >= lo && w.Idx <= hi {
				parts = append(parts, w.S)
			}
		}
	}
	return strings.Join(parts, " ")
}

// ------------------------------------------------------- highlight popover

// hlPalette is the 5-colour highlight palette (light tints keep black text
// readable on the Kaleido panel).
var hlPalette = []struct {
	Name string
	C    color.RGBA
}{
	{"yellow", color.RGBA{255, 232, 110, 255}},
	{"green", color.RGBA{170, 230, 150, 255}},
	{"blue", color.RGBA{150, 205, 245, 255}},
	{"pink", color.RGBA{248, 175, 205, 255}},
	{"orange", color.RGBA{250, 195, 130, 255}},
}

// lastHlColor remembers the most recently chosen colour across selections.
var lastHlColor = 0

func hlColorByName(n string) color.RGBA {
	for _, e := range hlPalette {
		if e.Name == n {
			return e.C
		}
	}
	return hlPalette[0].C
}

// hlPopover floats near the selection: pick a colour, then save as a plain
// highlight or attach a note (pen first, switchable to keyboard).
type hlPopover struct {
	art      *Article
	lo, hi   int
	rect     image.Rectangle
	swatches []image.Rectangle
	btns     []*Button
	save     func(colorName string, note *Note)
}

func (s *Article) showSelectionActions(a *App, lo, hi int) {
	// bounding box of the selection on the current page
	var selR image.Rectangle
	for _, w := range s.pages[s.cur].words {
		if w.Idx >= lo && w.Idx <= hi {
			selR = selR.Union(w.R)
		}
	}
	const pw, ph = 640, 262
	px := clamp(selR.Min.X+selR.Dx()/2-pw/2, 20, a.W-20-pw)
	py := selR.Max.Y + 16
	if py+ph > s.bottom {
		py = selR.Min.Y - ph - 16
	}
	if py < StatusBarH+10 {
		py = StatusBarH + 10
	}
	m := &hlPopover{art: s, lo: lo, hi: hi, rect: image.Rect(px, py, px+pw, py+ph)}

	m.save = func(colorName string, note *Note) {
		a.Store.Add(Highlight{
			Article: s.def.ID, Start: lo, End: hi,
			Text:  s.selectedText(lo, hi),
			Color: colorName,
			Note:  note,
		})
		s.selAnchor = -1
		a.CloseModal()
	}
	const sw = 92
	for i := range hlPalette {
		x := px + 28 + i*(sw+28)
		m.swatches = append(m.swatches, image.Rect(x, py+24, x+sw, py+24+sw))
	}
	by := py + 24 + sw + 24
	m.btns = []*Button{
		{R: image.Rect(px+28, by, px+28+300, by+90), Label: "+ note", Bold: true, Fill: true, OnTap: func() {
			pen := NewPenPad(a, "Pen note", nil, func() {
				// switch to keyboard
				a.ShowModal(NewKeyboard(a, "Typed note", func(text string) {
					if text == "" {
						s.selAnchor = -1
						a.Render(RefreshAuto)
						return
					}
					m.save("", &Note{Type: "text", Text: text})
				}))
			}, nil, func(strokes [][][2]int) {
				if strokes == nil {
					s.selAnchor = -1
					a.Render(RefreshAuto)
					return
				}
				m.save("", &Note{Type: "pen", Strokes: strokes})
			})
			a.ShowModal(pen)
		}},
		{R: image.Rect(px+pw-28-90, by, px+pw-28, by+90), Label: "x", OnTap: func() {
			s.selAnchor = -1
			a.CloseModal()
		}},
	}
	log.Printf("ui: highlight popover at %v", m.rect)
	a.ShowModal(m)
}

func (m *hlPopover) Rect() image.Rectangle { return m.rect }

func (m *hlPopover) Render(a *App) {
	c := a.D.Canvas()
	FillRect(c, m.rect.Inset(-4), 255)
	StrokeRect(c, m.rect, 4, 0)
	DrawStringTop(c, Small, "tap a colour to highlight", m.rect.Min.X+28, m.rect.Max.Y-118, 100)
	for i, r := range m.swatches {
		FillRGB(c, r, hlPalette[i].C)
		StrokeRect(c, r, 2, 0)
		if i == lastHlColor {
			StrokeRect(c, r.Inset(-8), 5, 0)
		}
	}
	for _, b := range m.btns {
		b.Draw(c)
	}
}

func (m *hlPopover) Touch(a *App, t Touch) {
	if t.Kind != TouchUp {
		return
	}
	pt := image.Pt(t.X, t.Y)
	for i, r := range m.swatches {
		if pt.In(r.Inset(-8)) {
			lastHlColor = i
			log.Printf("ui: highlight saved in %s", hlPalette[i].Name)
			m.save(hlPalette[i].Name, nil)
			return
		}
	}
	if hitButtons(a, t, m.btns) {
		return
	}
	if !pt.In(m.rect) {
		m.art.selAnchor = -1
		a.CloseModal()
	}
}

// openNoteEditor opens the pen pad (or keyboard for typed notes) directly on
// an existing highlight, pre-filled with its note, with Del to remove it.
func (s *Article) openNoteEditor(a *App, h Highlight) {
	title := h.Text
	if len(title) > 42 {
		title = title[:39] + "…"
	}
	title = "“" + title + "”"

	var initial [][][2]int
	initText := ""
	if h.Note != nil {
		if h.Note.Type == "pen" {
			initial = h.Note.Strokes
		} else {
			initText = h.Note.Text
		}
	}
	del := func() {
		a.Store.Delete(h.ID)
		a.CloseModal()
	}
	saveText := func(text string) {
		if text == "" {
			a.Render(RefreshAuto)
			return
		}
		h.Note = &Note{Type: "text", Text: text}
		a.Store.Update(h)
	}
	openKb := func() {
		kb := NewKeyboard(a, title, saveText)
		kb.Text = initText
		a.ShowModal(kb)
	}
	openPen := func() {
		pen := NewPenPad(a, title, initial, openKb, del, func(strokes [][][2]int) {
			if strokes == nil {
				a.Render(RefreshAuto)
				return
			}
			if len(strokes) == 0 {
				h.Note = nil
			} else {
				h.Note = &Note{Type: "pen", Strokes: strokes}
			}
			a.Store.Update(h)
		})
		a.ShowModal(pen)
	}
	if h.Note != nil && h.Note.Type == "text" {
		openKb()
	} else {
		openPen()
	}
}
