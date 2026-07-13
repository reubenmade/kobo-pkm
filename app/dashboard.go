package main

import (
	"fmt"
	"image"
	"time"
)

type Dashboard struct {
	btns    []*Button
	hlIdx   int
	done    map[int]bool
	todoBox []image.Rectangle
	hlRect  image.Rectangle
}

func NewDashboard() *Dashboard {
	return &Dashboard{done: map[int]bool{}}
}

func (d *Dashboard) Render(a *App) {
	c := a.D.Canvas()
	m := 60 // margin
	w := a.W - 2*m
	DrawStatusBar(a)
	y := StatusBarH + 24

	// header: date + weather
	now := time.Now()
	DrawStringTop(c, H1, now.Format("Monday 2 January"), m, y, 0)
	y += H1.Line
	DrawStringTop(c, Body, "18°C clear — light SW breeze (mock)", m, y, 0)
	y += Body.Line + 10
	HLine(c, m, a.W-m, y, 5, 0)
	y += 30

	d.btns = nil
	// exit button top-right
	d.btns = append(d.btns, &Button{
		R: image.Rect(a.W-m-150, StatusBarH+14, a.W-m, StatusBarH+84), Label: "exit",
		OnTap: func() { a.Quit = true },
	})

	// today
	y = d.section(a, "TODAY", y)
	for _, e := range mockEvents {
		DrawStringTop(c, Body, e, m+20, y, 0)
		y += Body.Line
	}
	y += 24

	// todos (tappable)
	y = d.section(a, "TODO", y)
	d.todoBox = d.todoBox[:0]
	for i, t := range mockTodos {
		box := image.Rect(m+20, y+6, m+20+36, y+42)
		d.todoBox = append(d.todoBox, image.Rect(m, y, a.W-m, y+Body.Line))
		StrokeRect(c, box, 3, 0)
		if d.done[i] {
			Line(c, box.Min.X+6, box.Min.Y+6, box.Max.X-6, box.Max.Y-6, 4, 0)
			Line(c, box.Max.X-6, box.Min.Y+6, box.Min.X+6, box.Max.Y-6, 4, 0)
		}
		DrawStringTop(c, Body, t, m+80, y, 0)
		if d.done[i] {
			HLine(c, m+80, m+80+Measure(Body, t), y+Body.Asc*2/3, 3, 0)
		}
		y += Body.Line
	}
	y += 24

	// current reads
	y = d.section(a, "CURRENT READS", y)
	for _, r := range mockReads {
		DrawStringTop(c, Bold, r.Title, m+20, y, 0)
		y += Bold.Line
		bar := image.Rect(m+20, y+4, a.W-m-20, y+22)
		StrokeRect(c, bar, 2, 0)
		FillRect(c, image.Rect(bar.Min.X, bar.Min.Y, bar.Min.X+bar.Dx()*r.Pct/100, bar.Max.Y), 0)
		DrawStringTop(c, Small, fmt.Sprintf("%d%%", r.Pct), a.W-m-90, y+28, 0)
		y += 66
	}
	y += 10

	// highlight loop (tap to cycle)
	y = d.section(a, "HIGHLIGHT LOOP  (tap for next)", y)
	hl := mockLoop[d.hlIdx%len(mockLoop)]
	top := y
	y = WrapDraw(c, BodyIt, "“"+hl.Text+"”", m+20, y, w-40, 0)
	DrawStringTop(c, Small, "— "+hl.Src, m+20, y, 0)
	y += Small.Line + 20
	d.hlRect = image.Rect(m, top-10, a.W-m, y)

	// next -> index
	d.btns = append(d.btns, &Button{
		R: image.Rect(a.W-m-360, a.H-160, a.W-m, a.H-60), Label: "reading queue  →", Bold: true, Fill: true,
		OnTap: func() { a.Push(NewIndex()) },
	})
	for _, b := range d.btns {
		b.Draw(c)
	}
}

func (d *Dashboard) section(a *App, title string, y int) int {
	DrawStringTop(a.D.Canvas(), H2, title, 60, y, 0)
	y += H2.Line + 4
	HLine(a.D.Canvas(), 60, a.W-60, y-8, 2, 100)
	return y + 8
}

func (d *Dashboard) Touch(a *App, t Touch) {
	if hitButtons(a, t, d.btns) {
		return
	}
	if t.Kind != TouchUp {
		return
	}
	pt := image.Pt(t.X, t.Y)
	for i, r := range d.todoBox {
		if pt.In(r) {
			d.done[i] = !d.done[i]
			d.Render(a)
			a.D.Refresh(r, RefreshFast)
			return
		}
	}
	if pt.In(d.hlRect) {
		d.hlIdx++
		d.Render(a)
		a.D.Refresh(d.hlRect, RefreshAuto)
	}
}
