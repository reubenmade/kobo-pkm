package main

import (
	"fmt"
	"image"
	"time"
)

// StatusBarH is the height reserved at the top of every screen.
const StatusBarH = 56

// DrawStatusBar renders time / frontlight / battery across the top.
// Returns the bar's rect so callers can partial-refresh just this region.
func DrawStatusBar(a *App) image.Rectangle {
	c := a.D.Canvas()
	r := image.Rect(0, 0, a.W, StatusBarH)
	FillRect(c, r, 255)

	DrawStringTop(c, Body, time.Now().Format("Mon 2 Jan  15:04"), 24, 8, 0)

	if fl, ok := FrontlightPercent(); ok {
		s := fmt.Sprintf("light %d%%  [tap]", fl)
		DrawStringTop(c, Small, s, a.W/2-Measure(Small, s)/2, 14, 0)
	}

	if pct, charging := BatteryStatus(); pct >= 0 {
		// battery icon: outline + nub + fill level
		bx := a.W - 24 - 54
		br := image.Rect(bx, 16, bx+48, 40)
		StrokeRect(c, br, 2, 0)
		FillRect(c, image.Rect(br.Max.X, 22, br.Max.X+5, 34), 0)
		inner := br.Inset(4)
		FillRect(c, image.Rect(inner.Min.X, inner.Min.Y, inner.Min.X+inner.Dx()*pct/100, inner.Max.Y), 0)
		label := fmt.Sprintf("%d%%", pct)
		if charging {
			label = "+" + label
		}
		DrawStringTop(c, Small, label, bx-Measure(Small, label)-10, 14, 0)
	}

	HLine(c, 0, a.W, r.Max.Y-2, 2, 140)
	return r
}

// HandleStatusBarTap processes taps on the bar. Tapping the light indicator
// cycles frontlight brightness. Returns true if the tap was consumed.
func HandleStatusBarTap(a *App, t Touch) bool {
	if t.Y >= StatusBarH {
		return false
	}
	if t.X > a.W/2-180 && t.X < a.W/2+180 {
		cur, ok := FrontlightPercent()
		if !ok {
			return false
		}
		next := 0
		for _, lv := range []int{25, 50, 75, 100} {
			if cur < lv-5 {
				next = lv
				break
			}
		}
		SetFrontlight(next)
		if s := a.top(); s != nil {
			s.Render(a)
		}
		a.D.Refresh(image.Rect(0, 0, a.W, StatusBarH), RefreshFast)
		return true
	}
	return false
}
