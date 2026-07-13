package main

import (
	"image"
	"log"
	"strings"
	"time"
)

type Button struct {
	R     image.Rectangle
	Label string
	Bold  bool
	Fill  bool // black background, white text
	OnTap func()
}

func (b *Button) Draw(c *image.RGBA) {
	f := Body
	if b.Bold {
		f = Bold
	}
	if b.Fill {
		FillRect(c, b.R, 0)
	} else {
		FillRect(c, b.R, 255)
		StrokeRect(c, b.R, 3, 0)
	}
	g := uint8(0)
	if b.Fill {
		g = 255
	}
	tw := Measure(f, b.Label)
	DrawString(c, f, b.Label, b.R.Min.X+(b.R.Dx()-tw)/2, b.R.Min.Y+(b.R.Dy()+f.Asc)/2-4, g)
}

func hitButtons(a *App, t Touch, btns []*Button) bool {
	if t.Kind != TouchUp {
		return false
	}
	for _, b := range btns {
		if b.OnTap != nil && image.Pt(t.X, t.Y).In(b.R) {
			log.Printf("ui: button %q tapped at (%d,%d)", b.Label, t.X, t.Y)
			// Flash the button for tactile feedback on e-ink.
			InvertRect(a.D.Canvas(), b.R)
			a.D.Refresh(b.R, RefreshFast)
			b.OnTap()
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- keyboard

// Keyboard is a modal on-screen keyboard filling the bottom of the screen.
type Keyboard struct {
	Title  string
	Text   string
	OnDone func(text string) // called with "" on cancel
	rect   image.Rectangle
	keys   []*kbKey
	caps   bool
}

type kbKey struct {
	R     image.Rectangle
	Label string
	Code  string // "char" payload or special: shift, bksp, done, cancel, space
}

func NewKeyboard(a *App, title string, onDone func(string)) *Keyboard {
	k := &Keyboard{Title: title, OnDone: onDone}
	k.rect = image.Rect(0, a.H*45/100, a.W, a.H)
	k.layout(a)
	return k
}

func (k *Keyboard) Rect() image.Rectangle { return k.rect }

func (k *Keyboard) layout(a *App) {
	k.keys = nil
	rows := []string{"1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm.,?"}
	pad := 8
	kw := (a.W - pad*11) / 10
	kh := 100
	y := k.rect.Min.Y + 140
	for _, row := range rows {
		x := (a.W - (kw+pad)*len(row) + pad) / 2
		for _, ch := range row {
			k.keys = append(k.keys, &kbKey{
				R: image.Rect(x, y, x+kw, y+kh), Label: string(ch), Code: string(ch),
			})
			x += kw + pad
		}
		y += kh + pad
	}
	// Bottom row: shift, space, backspace, cancel, done
	bw := (a.W - pad*6) / 5
	x := pad
	for _, sp := range [][2]string{{"aA", "shift"}, {"space", "space"}, {"del", "bksp"}, {"cancel", "cancel"}, {"DONE", "done"}} {
		k.keys = append(k.keys, &kbKey{R: image.Rect(x, y, x+bw, y+kh), Label: sp[0], Code: sp[1]})
		x += bw + pad
	}
}

func (k *Keyboard) fieldRect(a *App) image.Rectangle {
	return image.Rect(30, k.rect.Min.Y+64, a.W-30, k.rect.Min.Y+128)
}

func (k *Keyboard) Render(a *App) {
	c := a.D.Canvas()
	FillRect(c, k.rect, 255)
	HLine(c, 0, a.W, k.rect.Min.Y, 5, 0)
	DrawStringTop(c, H2, k.Title, 30, k.rect.Min.Y+16, 0)
	fr := k.fieldRect(a)
	StrokeRect(c, fr, 2, 0)
	txt := k.Text
	for Measure(Body, txt+"_") > fr.Dx()-20 && len(txt) > 0 {
		txt = txt[1:]
	}
	DrawString(c, Body, txt+"_", fr.Min.X+10, fr.Min.Y+(fr.Dy()+Body.Asc)/2-4, 0)
	for _, key := range k.keys {
		lbl := key.Label
		if k.caps && key.Code != "shift" && len(key.Code) == 1 {
			lbl = strings.ToUpper(lbl)
		}
		StrokeRect(c, key.R, 2, 0)
		tw := Measure(Key, lbl)
		DrawString(c, Key, lbl, key.R.Min.X+(key.R.Dx()-tw)/2, key.R.Min.Y+(key.R.Dy()+Key.Asc)/2-4, 0)
	}
}

// Touch fires on touch-DOWN (like a real keyboard) and issues exactly one
// e-ink refresh per keystroke so fast typing can't back up the queue.
func (k *Keyboard) Touch(a *App, t Touch) {
	if t.Kind != TouchDown {
		return
	}
	for _, key := range k.keys {
		if !image.Pt(t.X, t.Y).In(key.R) {
			continue
		}
		switch key.Code {
		case "shift":
			k.caps = !k.caps
			k.Render(a)
			a.D.Refresh(k.rect, RefreshFast)
			return
		case "space":
			k.Text += " "
		case "bksp":
			if len(k.Text) > 0 {
				k.Text = k.Text[:len(k.Text)-1]
			}
		case "cancel":
			a.CloseModal()
			k.OnDone("")
			return
		case "done":
			a.CloseModal()
			k.OnDone(strings.TrimSpace(k.Text))
			return
		default:
			ch := key.Code
			if k.caps {
				ch = strings.ToUpper(ch)
			}
			k.Text += ch
		}
		// One refresh: flash the key + updated text field together. The
		// canvas invert is undone immediately after (no extra refresh) so
		// the flash clears on the next update that touches the key.
		c := a.D.Canvas()
		InvertRect(c, key.R)
		fr := k.fieldRect(a)
		FillRect(c, fr, 255)
		StrokeRect(c, fr, 2, 0)
		txt := k.Text
		for Measure(Body, txt+"_") > fr.Dx()-20 && len(txt) > 0 {
			txt = txt[1:]
		}
		DrawString(c, Body, txt+"_", fr.Min.X+10, fr.Min.Y+(fr.Dy()+Body.Asc)/2-4, 0)
		a.D.Refresh(fr.Union(key.R), RefreshPen)
		InvertRect(c, key.R)
		return
	}
}

// ---------------------------------------------------------------- pen pad

// PenPad is a modal scribble area. Strokes are stored as raw point series
// in a *virtual* canvas taller than the visible window (scroll with up/dn
// for long notes) — exactly what a server-side handwriting-to-text pipeline
// wants as input.
type PenPad struct {
	Title    string
	OnDone   func(strokes [][][2]int) // nil strokes on cancel
	OnSwitch func()                   // optional: switch to typed note
	OnDelete func()                   // optional: delete the parent highlight
	rect     image.Rectangle
	area     image.Rectangle // visible drawing window
	strokes  [][][2]int      // virtual coords (y includes scroll offset)
	cur      [][2]int
	last     [2]int
	btns     []*Button
	// Refresh throttling: ink is drawn to the canvas on every move (cheap),
	// but the e-ink update ioctl (~100ms) runs at most ~16x/sec.
	dirty     image.Rectangle
	lastFlush time.Time
	yOff      int // scroll offset into the virtual canvas
	noteH     int // virtual canvas height
}

func NewPenPad(a *App, title string, initial [][][2]int, onSwitch, onDelete func(), onDone func([][][2]int)) *PenPad {
	p := &PenPad{Title: title, OnDone: onDone, OnSwitch: onSwitch, OnDelete: onDelete, strokes: initial}
	p.rect = image.Rect(0, a.H*40/100, a.W, a.H)
	p.area = image.Rect(20, p.rect.Min.Y+130, a.W-20, a.H-140)
	p.noteH = p.area.Dy() * 3

	type action struct {
		label string
		bold  bool
		fill  bool
		fn    func()
	}
	acts := []action{
		{"Clear", false, false, func() {
			p.strokes = nil
			p.repaint(a)
		}},
		{"up", false, false, func() { p.scroll(a, -1) }},
		{"dn", false, false, func() { p.scroll(a, +1) }},
	}
	if onSwitch != nil {
		acts = append(acts, action{"abc", false, false, onSwitch})
	}
	if onDelete != nil {
		acts = append(acts, action{"Del", false, false, onDelete})
	}
	acts = append(acts,
		action{"Cancel", false, false, func() { a.CloseModal(); p.OnDone(nil) }},
		action{"Save", true, true, func() { a.CloseModal(); p.OnDone(p.strokes) }},
	)
	// lay the buttons out evenly along the bottom row
	n := len(acts)
	bw := (a.W - 40 - (n-1)*20) / n
	y := a.H - 120
	for i, ac := range acts {
		x := 20 + i*(bw+20)
		p.btns = append(p.btns, &Button{
			R: image.Rect(x, y, x+bw, y+90), Label: ac.label, Bold: ac.bold, Fill: ac.fill, OnTap: ac.fn,
		})
	}
	return p
}

func (p *PenPad) Rect() image.Rectangle { return p.rect }

func (p *PenPad) repaint(a *App) {
	p.Render(a)
	a.D.Refresh(p.rect, RefreshAuto)
}

func (p *PenPad) scroll(a *App, dir int) {
	p.yOff = clamp(p.yOff+dir*p.area.Dy()*2/3, 0, p.noteH-p.area.Dy())
	p.repaint(a)
}

func (p *PenPad) Render(a *App) {
	c := a.D.Canvas()
	FillRect(c, p.rect, 255)
	HLine(c, 0, a.W, p.rect.Min.Y, 5, 0)
	DrawStringTop(c, H2, p.Title, 30, p.rect.Min.Y+16, 0)
	DrawStringTop(c, Small, "write below · up/dn to scroll for longer notes", 30, p.rect.Min.Y+70, 0)
	StrokeRect(c, p.area, 2, 0)
	// strokes intersecting the current view window
	for _, s := range p.strokes {
		for i := 1; i < len(s); i++ {
			y0 := s[i-1][1] - p.yOff
			y1 := s[i][1] - p.yOff
			if (y0 < 0 && y1 < 0) || (y0 > p.area.Dy() && y1 > p.area.Dy()) {
				continue
			}
			Line(c, p.area.Min.X+s[i-1][0], p.area.Min.Y+y0,
				p.area.Min.X+s[i][0], p.area.Min.Y+y1, 6, 0)
		}
	}
	// scroll indicator on the right edge
	if p.noteH > p.area.Dy() {
		track := image.Rect(p.area.Max.X-14, p.area.Min.Y, p.area.Max.X, p.area.Max.Y)
		StrokeRect(c, track, 1, 120)
		th := p.area.Dy() * p.area.Dy() / p.noteH
		ty := p.area.Min.Y + p.yOff*(p.area.Dy()-th)/(p.noteH-p.area.Dy())
		FillRect(c, image.Rect(track.Min.X+3, ty, track.Max.X-3, ty+th), 0)
	}
	for _, b := range p.btns {
		b.Draw(c)
	}
}

// clampToArea snaps a point into the drawing area, so strokes that graze
// the border keep inking instead of silently dying.
func (p *PenPad) clampToArea(x, y int) (int, int) {
	return clamp(x, p.area.Min.X+3, p.area.Max.X-18), clamp(y, p.area.Min.Y+3, p.area.Max.Y-4)
}

func (p *PenPad) Touch(a *App, t Touch) {
	pt := image.Pt(t.X, t.Y)
	switch t.Kind {
	case TouchDown:
		// Forgiving start zone: anywhere near the pad (but not the buttons).
		if pt.In(p.area.Inset(-50)) && t.Y < p.btns[0].R.Min.Y-10 {
			x, y := p.clampToArea(t.X, t.Y)
			p.cur = [][2]int{{x - p.area.Min.X, y - p.area.Min.Y + p.yOff}}
			p.last = [2]int{x, y}
		}
	case TouchMove:
		if p.cur != nil {
			x, y := p.clampToArea(t.X, t.Y)
			if abs(x-p.last[0])+abs(y-p.last[1]) < 3 {
				return // jitter filter
			}
			Line(a.D.Canvas(), p.last[0], p.last[1], x, y, 6, 0)
			seg := image.Rect(p.last[0], p.last[1], x, y).Canon().Inset(-8)
			p.dirty = p.dirty.Union(seg)
			if time.Since(p.lastFlush) >= 60*time.Millisecond {
				a.D.Refresh(p.dirty, RefreshPen)
				p.dirty = image.Rectangle{}
				p.lastFlush = time.Now()
			}
			p.cur = append(p.cur, [2]int{x - p.area.Min.X, y - p.area.Min.Y + p.yOff})
			p.last = [2]int{x, y}
		}
	case TouchUp:
		if p.cur != nil {
			if len(p.cur) > 1 {
				p.strokes = append(p.strokes, p.cur)
				log.Printf("pen: stroke done, %d points (%d strokes total)", len(p.cur), len(p.strokes))
			}
			p.cur = nil
			if !p.dirty.Empty() {
				a.D.Refresh(p.dirty, RefreshPen)
				p.dirty = image.Rectangle{}
			}
			// Settle the pad so the A2-rendered ink gets a clean grayscale pass.
			a.D.Refresh(p.area, RefreshAuto)
			return
		}
		hitButtons(a, t, p.btns)
	}
}

// ---------------------------------------------------------------- toast

func Toast(a *App, msg string) {
	c := a.D.Canvas()
	w := Measure(Body, msg) + 60
	r := image.Rect(a.W/2-w/2, a.H-90, a.W/2+w/2, a.H-20)
	FillRect(c, r, 0)
	DrawString(c, Body, msg, r.Min.X+30, r.Min.Y+(r.Dy()+Body.Asc)/2-4, 255)
	a.D.Refresh(r, RefreshAuto)
}
