package main

import (
	"fmt"
	"image"
)

type Index struct {
	btns []*Button
}

func NewIndex() *Index { return &Index{} }

func (s *Index) Render(a *App) {
	c := a.D.Canvas()
	m := 60
	DrawStatusBar(a)
	y := StatusBarH + 24
	DrawStringTop(c, H1, "Reading queue", m, y, 0)
	y += H1.Line + 10
	HLine(c, m, a.W-m, y, 5, 0)
	y += 40

	s.btns = []*Button{{
		R: image.Rect(m, a.H-160, m+360, a.H-60), Label: "←  dashboard",
		OnTap: func() { a.Pop() },
	}}

	for i := range articles {
		art := articles[i]
		card := image.Rect(m, y, a.W-m, y+190)
		StrokeRect(c, card, 3, 0)
		DrawStringTop(c, Bold, art.Title, m+24, y+24, 0)
		DrawStringTop(c, Small, art.Byline, m+24, y+24+Bold.Line, 0)
		nHl := len(a.Store.ForArticle(art.ID))
		meta := "unread"
		if nHl > 0 {
			meta = fmt.Sprintf("%d highlight(s)", nHl)
		}
		DrawStringTop(c, Small, meta, m+24, y+24+Bold.Line+Small.Line+6, 60)
		s.btns = append(s.btns, &Button{
			R: card, Label: "", OnTap: func() { a.Push(NewArticle(a, art)) },
		})
		y += 220
	}

	DrawStringTop(c, Small, "articles arrive here from the listen-later server", m, y+20, 100)

	for _, b := range s.btns {
		if b.Label != "" {
			b.Draw(c)
		}
	}
}

func (s *Index) Touch(a *App, t Touch) {
	if t.Kind != TouchUp {
		return
	}
	for _, b := range s.btns {
		if image.Pt(t.X, t.Y).In(b.R) {
			b.OnTap()
			return
		}
	}
}
