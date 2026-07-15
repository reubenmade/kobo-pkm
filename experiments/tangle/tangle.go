package main

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"github.com/reubenmade/kobo-pkm/kit"
)

// A Bret-Victor-style "reactive document" (worrydream.com/Tangle): prose whose
// numbers are live. Hold the pen's side button and slide sideways over a bold
// number to scrub it; every number that depends on it updates and the page
// redraws. Two one-pagers, flipped with the physical page buttons:
//
//   Page 0 — Proposition 21: a civic what-if (surcharge × vehicles → parks
//            budget → attendance).
//   Page 1 — a state-variable filter ("the electrical"): cutoff Fc and
//            resonance Q reshape a frequency-response curve that redraws live.
//
// The copy is a faithful reconstruction — the originals are injected by
// JavaScript and can't be scraped — but the reactive relationships and the
// feel are the point.

// ---- the adjustable quantity ----------------------------------------------

// Var is one scrubbable number: a value with bounds, a snap step, how many
// pen-pixels equal one step, and how it formats. hit is filled in each render
// so the pen can find it.
type Var struct {
	Val, Min, Max, Step float64
	PxPerStep           float64
	Format              func(float64) string
	hit                 image.Rectangle
}

func (v *Var) text() string { return v.Format(v.Val) }

func (v *Var) setFromScrub(startVal float64, dxPixels int) bool {
	steps := math.Round(float64(dxPixels) / v.PxPerStep)
	nv := startVal + steps*v.Step
	nv = math.Max(v.Min, math.Min(v.Max, nv))
	// snap to the step grid
	nv = math.Round(nv/v.Step) * v.Step
	if nv == v.Val {
		return false
	}
	v.Val = nv
	return true
}

// ---- the document ---------------------------------------------------------

const (
	margin    = 74
	pageCount = 2
	// e-ink discipline: never refresh faster than this while scrubbing.
	refreshEvery = 60 * time.Millisecond
)

type Doc struct {
	c      *image.RGBA
	bounds image.Rectangle
	page   int

	// Page 0 — Proposition 21.
	surcharge *Var
	vehicles  *Var

	// Page 1 — the state-variable filter.
	fc *Var
	q  *Var

	// interaction state
	penX, penY  int
	hover       *Var
	scrub       *Var
	scrubStartX int
	scrubStart  float64

	// throttled refresh bookkeeping
	pending     kit.BBox
	pendingMode kit.RefreshMode
	havePending bool
	lastRefresh time.Time

	// filled each render, for dirty regions
	contentRect image.Rectangle
}

func NewDoc(c *image.RGBA, bounds image.Rectangle) *Doc {
	d := &Doc{c: c, bounds: bounds}
	d.surcharge = &Var{Val: 18, Min: 0, Max: 40, Step: 1, PxPerStep: 7,
		Format: func(v float64) string { return fmt.Sprintf("$%.0f", v) }}
	d.vehicles = &Var{Val: 28, Min: 8, Max: 35, Step: 1, PxPerStep: 11,
		Format: func(v float64) string { return fmt.Sprintf("%.0f million", v) }}
	d.fc = &Var{Val: 1000, Min: 100, Max: 8000, Step: 50, PxPerStep: 3,
		Format: func(v float64) string { return fmt.Sprintf("%.0f Hz", v) }}
	d.q = &Var{Val: 2.0, Min: 0.5, Max: 20, Step: 0.1, PxPerStep: 4,
		Format: func(v float64) string { return fmt.Sprintf("%.1f", v) }}
	return d
}

func (d *Doc) Page() int { return d.page }

func (d *Doc) SetPage(p int) {
	d.page = ((p % pageCount) + pageCount) % pageCount
	d.scrub, d.hover = nil, nil
	d.Render()
}

func (d *Doc) NextPage() { d.SetPage(d.page + 1) }
func (d *Doc) PrevPage() { d.SetPage(d.page - 1) }

func (d *Doc) curVars() []*Var {
	if d.page == 0 {
		return []*Var{d.surcharge, d.vehicles}
	}
	return []*Var{d.fc, d.q}
}

// ---- the Prop 21 model ----------------------------------------------------

const (
	prop21GeneralFund = 130.0 // $M the general fund spends on parks today
	prop21BaseVisits  = 68.0  // million park visits/year before the measure
)

func (d *Doc) revenue() float64  { return d.surcharge.Val * d.vehicles.Val } // $M/yr
func (d *Doc) leftover() float64 { return d.revenue() - prop21GeneralFund }
func (d *Doc) attendUplift() float64 {
	// Free admission lifts attendance in proportion to the revenue raised
	// (nicer, better-staffed parks); calibrated so the default raises 21%.
	return 0.21 * d.revenue() / 504.0
}
func (d *Doc) visits() float64 { return prop21BaseVisits * (1 + d.attendUplift()) }

// ---- the filter model -----------------------------------------------------

// lowpassDB is the magnitude response of a 2nd-order (state-variable) lowpass
// at frequency f, in dB, for cutoff fc and resonance q.
func lowpassDB(f, fc, q float64) float64 {
	r := f / fc
	denom := math.Sqrt(math.Pow(1-r*r, 2) + math.Pow(r/q, 2))
	if denom < 1e-9 {
		denom = 1e-9
	}
	return 20 * math.Log10(1/denom)
}

// peakBoostDB is roughly how much the resonance lifts the cutoff band.
func (d *Doc) peakBoostDB() float64 {
	if d.q.Val <= 0.71 {
		return 0
	}
	return 20 * math.Log10(d.q.Val)
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func (d *Doc) Render() {
	kit.FillRect(d.c, d.bounds, kit.WHITE)
	x := margin
	w := d.bounds.Dx() - 2*margin

	if d.page == 0 {
		d.renderProp21(x, w)
	} else {
		d.renderFilter(x, w)
	}
	d.renderFooter()
}

func (d *Doc) header(x, w int, title, subtitle string) int {
	kit.DrawStringTop(d.c, kit.H1, title, x, 56, kit.BLACK)
	kit.DrawStringTop(d.c, kit.Small, subtitle, x, 56+kit.H1.Line, kit.GRAY)
	ruleY := 56 + kit.H1.Line + kit.Small.Line + 18
	kit.FillRect(d.c, image.Rect(x, ruleY, x+w, ruleY+2), kit.BLACK)
	return ruleY + 40
}

func (d *Doc) renderProp21(x, w int) {
	top := d.header(x, w, "Proposition 21", "hold the pen button + slide a bold number")

	rev := fmt.Sprintf("$%.0f million", d.revenue())
	left := money(d.leftover())
	up := d.attendUplift()
	dir := "rise"
	if up < 0 {
		dir = "fall"
	}
	risePct := fmt.Sprintf("%.0f%%", math.Abs(up*100))
	vis := fmt.Sprintf("%.0f million", d.visits())

	y := top
	y = d.flow(x, y, w, []tok{
		st("Proposition 21 would add a"), vr(d.surcharge),
		st("annual surcharge to each of California's"), vr(d.vehicles),
		st("registered vehicles."),
	})
	y += kit.Body.Line / 2
	y = d.flow(x, y, w, []tok{
		st("That raises"), bd(rev),
		st("a year for the state parks — enough to cover the"), bd(money(prop21GeneralFund)),
		st("the general fund spends on them today, with"), bd(left),
		st("left over for trails, repairs, and new land."),
	})
	y += kit.Body.Line / 2
	y = d.flow(x, y, w, []tok{
		st("In exchange, every registered vehicle gets in free. Park attendance, today about"),
		bd(money2(prop21BaseVisits, "million visits")),
		st("a year, would"), bd(dir), st("by"), bd(risePct),
		st("to"), bd(vis), st("visits."),
	})

	d.contentRect = image.Rect(x-20, top-10, x+w+20, y+10)
}

func (d *Doc) renderFilter(x, w int) {
	top := d.header(x, w, "A State-Variable Filter", "hold the pen button + slide a bold number")

	y := top
	y = d.flow(x, y, w, []tok{
		st("This filter passes low tones and rejects high ones. The cutoff"), vr(d.fc),
		st("sets where the roll-off begins; the resonance Q ="), vr(d.q),
		st("sets how sharply it peaks there —"), bd(fmt.Sprintf("+%.1f dB", d.peakBoostDB())),
		st("of boost right at the corner."),
	})
	y += kit.Body.Line / 2

	// The frequency-response plot — this is what redraws live as Q is scrubbed.
	plot := image.Rect(x, y, x+w, y+560)
	d.renderResponse(plot)

	d.contentRect = image.Rect(x-20, top-10, x+w+20, plot.Max.Y+20)
}

// renderResponse draws the magnitude response curve on a log-frequency axis.
func (d *Doc) renderResponse(r image.Rectangle) {
	const (
		fLo  = 20.0
		fHi  = 20000.0
		dbHi = 24.0
		dbLo = -42.0
	)
	kit.Frame(d.c, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), 2, kit.BLACK)

	xForF := func(f float64) int {
		t := math.Log10(f/fLo) / math.Log10(fHi/fLo)
		return r.Min.X + int(t*float64(r.Dx()-1))
	}
	yForDB := func(db float64) int {
		t := (dbHi - db) / (dbHi - dbLo)
		return r.Min.Y + kit.Clamp(int(t*float64(r.Dy()-1)), 0, r.Dy()-1)
	}

	// Decade gridlines + labels.
	for _, f := range []float64{100, 1000, 10000} {
		gx := xForF(f)
		for gy := r.Min.Y + 4; gy < r.Max.Y-4; gy += 10 {
			kit.PutPx(d.c, gx, gy, kit.LGRAY)
			kit.PutPx(d.c, gx, gy+1, kit.LGRAY)
		}
		lbl := map[float64]string{100: "100 Hz", 1000: "1 kHz", 10000: "10 kHz"}[f]
		kit.DrawStringTop(d.c, kit.Small, lbl, gx+6, r.Max.Y-kit.Small.Line-4, kit.GRAY)
	}
	// dB gridlines + labels.
	for _, db := range []float64{18, 0, -18, -36} {
		gy := yForDB(db)
		for gx := r.Min.X + 4; gx < r.Max.X-4; gx += 10 {
			kit.PutPx(d.c, gx, gy, kit.LGRAY)
		}
		kit.DrawStringTop(d.c, kit.Small, fmt.Sprintf("%+.0f", db), r.Min.X+8, gy-kit.Small.Line/2, kit.GRAY)
	}
	// 0 dB reference line, solid.
	zeroY := yForDB(0)
	kit.FillRect(d.c, image.Rect(r.Min.X+2, zeroY, r.Max.X-2, zeroY+1), kit.GRAY)

	// Cutoff marker: a dashed vertical line at Fc that slides as Fc is scrubbed.
	cx := xForF(d.fc.Val)
	for gy := r.Min.Y + 4; gy < r.Max.Y-4; gy += 14 {
		kit.FillRect(d.c, image.Rect(cx, gy, cx+2, gy+7), kit.BLACK)
	}

	// The response curve.
	prevY := 0
	for px := 0; px < r.Dx(); px++ {
		t := float64(px) / float64(r.Dx()-1)
		f := fLo * math.Pow(fHi/fLo, t)
		db := lowpassDB(f, d.fc.Val, d.q.Val)
		gy := yForDB(db)
		gx := r.Min.X + px
		if px == 0 {
			prevY = gy
		}
		kit.BrushLine(d.c, gx-1, prevY, gx, gy, 1, kit.BLACK)
		prevY = gy
	}
}

func (d *Doc) renderFooter() {
	w := d.bounds.Dx()
	y := d.bounds.Dy() - 70
	kit.FillRect(d.c, image.Rect(margin, y-24, w-margin, y-22), kit.LGRAY)
	kit.DrawStringTop(d.c, kit.Small, "page buttons flip  -  3 taps top-corner exits", margin, y, kit.LGRAY)
	label := fmt.Sprintf("page %d of %d  >", d.page+1, pageCount)
	kit.DrawStringTop(d.c, kit.Small, label, w-margin-kit.Measure(kit.Small, label), y, kit.GRAY)
}

// ---- inline token flow ----------------------------------------------------

// tok is one item in a paragraph: static prose (v==nil, split into words), a
// scrubbable Var (v!=nil), or a bold derived value (bold==true, atomic).
type tok struct {
	s    string
	v    *Var
	bold bool
}

func st(s string) tok  { return tok{s: s} }
func vr(v *Var) tok    { return tok{v: v} }
func bd(s string) tok  { return tok{s: s, bold: true} }

// word is an atomic placed unit.
type word struct {
	s    string
	v    *Var
	bold bool
}

// flow lays a paragraph of tokens into lines of width maxW starting at (x, y),
// drawing as it goes and recording each Var's hit box. Returns the y below the
// last line.
func (d *Doc) flow(x, y, maxW int, toks []tok) int {
	var words []word
	for _, t := range toks {
		switch {
		case t.v != nil:
			words = append(words, word{s: t.v.text(), v: t.v, bold: true})
		case t.bold:
			words = append(words, word{s: t.s, bold: true})
		default:
			for _, w := range strings.Fields(t.s) {
				words = append(words, word{s: w})
			}
		}
	}

	space := kit.Measure(kit.Body, " ")
	baseline := y + kit.Body.Asc
	cx := x
	for _, wd := range words {
		face := kit.Body
		if wd.bold {
			face = kit.Bold
		}
		ww := kit.Measure(face, wd.s)
		if cx > x {
			if cx+space+ww > x+maxW {
				cx = x
				baseline += kit.Body.Line
			} else {
				cx += space
			}
		}
		g := kit.BLACK
		if wd.v != nil {
			top := baseline - kit.Body.Asc - 6
			bot := baseline + (kit.Body.Line - kit.Body.Asc) + 6
			wd.v.hit = image.Rect(cx-8, top, cx+ww+8, bot)
			active := wd.v == d.scrub || wd.v == d.hover
			if wd.v == d.scrub {
				kit.FillRect(d.c, wd.v.hit, kit.LGRAY)
			}
			kit.DrawString(d.c, face, wd.s, cx, baseline, g)
			if active {
				uy := baseline + 6
				kit.FillRect(d.c, image.Rect(cx-2, uy, cx+ww+2, uy+3), kit.BLACK)
			}
		} else {
			kit.DrawString(d.c, face, wd.s, cx, baseline, g)
		}
		cx += ww
	}
	return baseline + (kit.Body.Line - kit.Body.Asc)
}

// ---------------------------------------------------------------------------
// Interaction
// ---------------------------------------------------------------------------

// varAt returns the scrubbable var whose hit box contains (x, y), or nil.
func (d *Doc) varAt(x, y int) *Var {
	for _, v := range d.curVars() {
		if image.Pt(x, y).In(v.hit) {
			return v
		}
	}
	return nil
}

// HandleTouch feeds one event through the interaction model. It re-renders the
// canvas as needed and returns the region to refresh, the waveform, and whether
// anything should be pushed to the panel now (throttled for smoothness).
func (d *Doc) HandleTouch(t kit.Touch) (image.Rectangle, kit.RefreshMode, bool) {
	d.penX, d.penY = t.X, t.Y

	// Begin scrubbing: pen button pressed while over a bold number.
	if t.Button && d.scrub == nil {
		if v := d.varAt(t.X, t.Y); v != nil {
			d.scrub = v
			d.scrubStartX = t.X
			d.scrubStart = v.Val
			d.hover = v
			d.Render()
			return d.flush(true, kit.RefreshFast)
		}
	}

	// Continue scrubbing.
	if d.scrub != nil && t.Button {
		if d.scrub.setFromScrub(d.scrubStart, t.X-d.scrubStartX) {
			d.Render()
			d.mark(d.contentRect, kit.RefreshFast)
			return d.flush(false, kit.RefreshFast) // throttled
		}
		return empty()
	}

	// End scrubbing: button released — one clean settling pass.
	if d.scrub != nil && !t.Button {
		d.scrub = nil
		d.hover = d.varAt(t.X, t.Y)
		d.Render()
		return d.flush(true, kit.RefreshAuto)
	}

	// Plain hover: move the underline to whatever number the pen is over.
	nh := d.varAt(t.X, t.Y)
	if nh != d.hover {
		prev := d.hover
		d.hover = nh
		d.Render()
		dirty := kit.EmptyBBox()
		if prev != nil {
			dirty.AddRect(prev.hit)
		}
		if nh != nil {
			dirty.AddRect(nh.hit)
		}
		if !dirty.IsEmpty() {
			d.mark(dirty.Rect(d.bounds), kit.RefreshFast)
			return d.flush(true, kit.RefreshFast)
		}
	}
	return empty()
}

// Tick flushes any throttled change once the refresh window has elapsed, so a
// scrub that stops mid-slide still settles without another event.
func (d *Doc) Tick() (image.Rectangle, kit.RefreshMode, bool) {
	if d.havePending {
		return d.flush(false, d.pendingMode)
	}
	return empty()
}

func (d *Doc) mark(r image.Rectangle, mode kit.RefreshMode) {
	d.pending.AddRect(r)
	d.pendingMode = mode
	d.havePending = true
}

// flush returns a refresh if forced, or if the throttle window has elapsed.
func (d *Doc) flush(force bool, mode kit.RefreshMode) (image.Rectangle, kit.RefreshMode, bool) {
	if !d.havePending {
		if force {
			// nothing accumulated but caller wants a paint (e.g. hover)
			return d.contentRect, mode, true
		}
		return empty()
	}
	if !force && time.Since(d.lastRefresh) < refreshEvery {
		return empty()
	}
	r := d.pending.Rect(d.bounds)
	d.pending = kit.EmptyBBox()
	d.havePending = false
	d.lastRefresh = time.Now()
	return r, mode, true
}

func empty() (image.Rectangle, kit.RefreshMode, bool) {
	return image.Rectangle{}, kit.RefreshAuto, false
}

// ---- formatting helpers ---------------------------------------------------

func money(m float64) string {
	if m < 0 {
		return fmt.Sprintf("-$%.0f million", -m)
	}
	return fmt.Sprintf("$%.0f million", m)
}

func money2(v float64, unit string) string {
	return fmt.Sprintf("%.0f %s", v, unit)
}

// DrawSplash centers a big line and a small line (for sleep / exit screens).
func DrawSplash(c *image.RGBA, bounds image.Rectangle, big, small string) {
	kit.FillRect(c, bounds, kit.WHITE)
	kit.DrawCentered(c, kit.H1, big, 0, bounds.Dx(), bounds.Dy()/2-40, kit.BLACK)
	kit.DrawCentered(c, kit.Small, small, 0, bounds.Dx(), bounds.Dy()/2+40, kit.GRAY)
}
