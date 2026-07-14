package main

import "image"

// The diary's guide: a lone, large "?" drawn on the page summons a panel of
// the diary's gestures; touching the page dismisses it. Detection is local
// geometry — no oracle — so the guide works even with no network.
// Port of riddle's help.rs, gesture text adapted for the Kobo.

// LooksLikeQuestionMark: does the committed ink look like a single big "?"
// (with or without its dot)? Deliberately forgiving: a false positive only
// shows the guide.
func LooksLikeQuestionMark(strokes Strokes) bool {
	if len(strokes) == 0 || len(strokes) > 3 {
		return false
	}
	mainI := 0
	for i := range strokes {
		if len(strokes[i]) > len(strokes[mainI]) {
			mainI = i
		}
	}
	main := strokes[mainI]
	if len(main) < 12 {
		return false
	}
	x0, y0, x1, y1 := 1<<30, 1<<30, -(1 << 30), -(1 << 30)
	for _, p := range main {
		x0, y0 = min(x0, p.X), min(y0, p.Y)
		x1, y1 = max(x1, p.X), max(y1, p.Y)
	}
	w, h := x1-x0, y1-y0
	// Big, and taller than wide: a lone glyph, not a line of writing.
	if h < 280 || w < 70 || h < w {
		return false
	}
	// Any other stroke must be the dot: small, low, roughly under the glyph.
	for i, s := range strokes {
		if i == mainI {
			continue
		}
		dx0, dy0, dx1, dy1 := 1<<30, 1<<30, -(1 << 30), -(1 << 30)
		for _, p := range s {
			dx0, dy0 = min(dx0, p.X), min(dy0, p.Y)
			dx1, dy1 = max(dx1, p.X), max(dy1, p.Y)
		}
		if max(dx1-dx0, dy1-dy0) > 90 {
			return false
		}
		if (dy0+dy1)/2 < y0+h*60/100 {
			return false
		}
		if (dx0+dx1)/2 < x0-80 || (dx0+dx1)/2 > x1+80 {
			return false
		}
	}
	// Normalize to top-down drawing order.
	pts := make([]Pt, len(main))
	for i, p := range main {
		pts[i] = Pt{p.X, p.Y}
	}
	if pts[0].Y > pts[len(pts)-1].Y {
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
	}
	start, end := pts[0], pts[len(pts)-1]
	if start.Y > y0+h*40/100 || end.Y < y0+h*55/100 {
		return false
	}
	// The top arcs across most of the width…
	topMinX, topMaxX, topMaxXY := 1<<30, -(1 << 30), 0
	for _, p := range pts {
		if p.Y <= y0+h*45/100 {
			if p.X > topMaxX {
				topMaxX, topMaxXY = p.X, p.Y
			}
			topMinX = min(topMinX, p.X)
		}
	}
	if topMaxX == -(1<<30) || topMaxX-topMinX < w*55/100 {
		return false
	}
	// …and comes back DOWN on the right (rules out the flat bar of a "7").
	if topMaxXY < y0+h*8/100 {
		return false
	}
	// The descender stays narrow.
	botMinX, botMaxX := 1<<30, -(1 << 30)
	for _, p := range pts {
		if p.Y >= y0+h*66/100 {
			botMinX = min(botMinX, p.X)
			botMaxX = max(botMaxX, p.X)
		}
	}
	if botMaxX != -(1<<30) && botMaxX-botMinX > w*60/100 {
		return false
	}
	return true
}

const helpTitle = "The Diary"

var helpBody = []string{
	"Write, then rest your pen:",
	"the diary drinks your ink and Tom replies.",
	"",
	"The diary remembers. Ask it:",
	"\"show me what I wrote about...\"",
	"and the page will rise again.",
	"",
	"Tap three times in a top corner to leave.",
	"The power button or cover sleeps the diary.",
	"",
	"A large ? summons this guide.",
}

const helpFooter = "Touch the page to close."

var (
	helpTitlePx  = 88.0
	helpBodyPx   = 54.0
	helpFooterPx = 40.0
)

const helpPad = 64

// Help is the open guide panel: remembers the pixels it covered.
type Help struct {
	Region BBox
	rect   image.Rectangle
	saved  []byte
}

// ShowHelp draws the guide panel centered on the page; returns it for later
// dismissal.
func ShowHelp(c *image.RGBA) *Help {
	W, H := c.Bounds().Dx(), c.Bounds().Dy()
	titleH := int(helpTitlePx * 1.4)
	lineH := int(helpBodyPx * 1.3)
	footerH := int(helpFooterPx * 1.4)

	wmax := MeasureScript(helpTitle, helpTitlePx)
	for _, l := range helpBody {
		wmax = max(wmax, MeasureScript(l, helpBodyPx))
	}
	pw := min(wmax+2*helpPad, W-40)
	ph := helpPad + titleH + lineH/2 + len(helpBody)*lineH + footerH + helpPad
	px := (W - pw) / 2
	py := max(H-ph, 0) / 2

	rect := image.Rect(px, py, px+pw, py+ph)
	saved := CopyRect(c, rect)
	FillRect(c, rect, WHITE)
	Frame(c, px, py, pw, ph, 4, BLACK)
	Frame(c, px+14, py+14, pw-28, ph-28, 1, BLACK)

	y := py + helpPad
	blitCentered(c, helpTitle, helpTitlePx, px, pw, y)
	y += titleH + lineH/2
	for _, l := range helpBody {
		if l != "" {
			blitCentered(c, l, helpBodyPx, px, pw, y)
		}
		y += lineH
	}
	blitCentered(c, helpFooter, helpFooterPx, px, pw, y)

	region := EmptyBBox()
	region.Add(px, py, 2)
	region.Add(px+pw, py+ph, 2)
	return &Help{Region: region, rect: rect, saved: saved}
}

// Dismiss puts back what the panel covered; returns the region to refresh.
func (h *Help) Dismiss(c *image.RGBA) BBox {
	PasteRect(c, h.rect, h.saved)
	return h.Region
}

// ShowSleep replaces the page with the full-screen sleep card; returns the
// saved page pixels so waking can restore them exactly.
func ShowSleep(c *image.RGBA) []byte {
	b := c.Bounds()
	W, H := b.Dx(), b.Dy()
	saved := CopyRect(c, b)
	FillRect(c, b, WHITE)
	Frame(c, 48, 48, W-96, H-96, 4, BLACK)
	Frame(c, 66, 66, W-132, H-132, 1, BLACK)
	y := H * 38 / 100
	blitCentered(c, "The diary sleeps.", 116.0, 0, W, y)
	blitCentered(c, "Press the button to wake it.", 56.0, 0, W, y+230)
	return saved
}

func RestoreSleep(c *image.RGBA, saved []byte) {
	PasteRect(c, c.Bounds(), saved)
}

func blitCentered(c *image.RGBA, text string, px float64, panelX, panelW, y int) {
	line := RasterizeLine(text, px)
	x := panelX + max(panelW-line.W, 0)/2
	line.Blit(c, x, y, BLACK)
}
