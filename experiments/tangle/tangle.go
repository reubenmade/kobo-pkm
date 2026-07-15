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
// number to scrub it; a popover shows the value, every number that depends on
// it updates, and the page redraws. Three one-pagers, flipped with the
// physical page buttons:
//
//   Page 0 — Proposition 21: a civic what-if (surcharge × vehicles → parks
//            budget → attendance).
//   Page 1 — a state-variable filter ("the electrical"): cutoff Fc and
//            resonance Q reshape the low/band/high-pass response curves, with a
//            block diagram of the filter.
//   Page 2 — Ten Brighter Ideas, No. 3 (efficient appliances): efficiency and
//            adoption → energy, CO2, money saved, and reactors displaced.
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
	pageCount = 3
	// Default scrub pacing (Prop 21 / Brighter pages, which redraw with DU) —
	// roughly how long a DU update takes to paint, so we don't out-issue the
	// panel. The filter page overrides this per ghosting variant.
	refreshEvery = 220 * time.Millisecond
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

	// Page 2 — Ten Brighter Ideas No. 3.
	reduction *Var
	adoption  *Var

	// interaction state
	penX, penY  int
	hover       *Var
	scrub       *Var
	scrubStartX int
	scrubStart  float64

	// Ghosting lab (filter page): the selected redraw variant, the tap-to-cycle
	// and flash control hit boxes, and the two regions that actually change
	// during a scrub (so the static prose + diagram aren't re-driven and can't
	// ghost).
	variant    int
	variantHit image.Rectangle
	flashHit   image.Rectangle
	numberBand image.Rectangle // full-width strip over the Fc/Q line(s)
	plotRect   image.Rectangle

	// Reactor info popover (Brighter page): its hover target, and whether it's
	// currently shown.
	reactorHit image.Rectangle
	info       bool

	// Lazy render: scrub events update the value cheaply and mark needRender;
	// the actual (expensive) Render happens only at the throttle window or on
	// release — so a fast drag jumps straight to the final value.
	needRender  bool
	pendingMode kit.RefreshMode
	lastRefresh time.Time

	// filled each render, for dirty regions
	contentRect image.Rectangle
	overlayRect image.Rectangle // info popover, folded into the dirty region
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
	d.reduction = &Var{Val: 20, Min: 0, Max: 45, Step: 1, PxPerStep: 9, Format: pct0}
	d.adoption = &Var{Val: 60, Min: 0, Max: 100, Step: 1, PxPerStep: 5, Format: pct0}
	return d
}

func pct0(v float64) string { return fmt.Sprintf("%.0f%%", v) }

func (d *Doc) Page() int { return d.page }

func (d *Doc) SetPage(p int) {
	d.page = ((p % pageCount) + pageCount) % pageCount
	d.scrub, d.hover = nil, nil
	d.Render()
}

func (d *Doc) NextPage() { d.SetPage(d.page + 1) }
func (d *Doc) PrevPage() { d.SetPage(d.page - 1) }

func (d *Doc) curVars() []*Var {
	switch d.page {
	case 0:
		return []*Var{d.surcharge, d.vehicles}
	case 1:
		return []*Var{d.fc, d.q}
	default:
		return []*Var{d.reduction, d.adoption}
	}
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

// A state-variable filter gives three outputs from one input at once. Their
// 2nd-order magnitude responses share a denominator; only the numerator
// differs. r = f/fc.
const (
	svfLow  = 0
	svfBand = 1
	svfHigh = 2
)

func svfMagDB(kind int, f, fc, q float64) float64 {
	r := f / fc
	den := math.Sqrt(math.Pow(1-r*r, 2) + math.Pow(r/q, 2))
	if den < 1e-9 {
		den = 1e-9
	}
	var num float64
	switch kind {
	case svfLow:
		num = 1
	case svfBand:
		num = r / q
	default: // svfHigh
		num = r * r
	}
	if num < 1e-12 {
		num = 1e-12
	}
	return 20 * math.Log10(num/den)
}

// peakBoostDB is roughly how much the resonance lifts the cutoff band (the
// low- and high-pass outputs peak by ~this much at Fc).
func (d *Doc) peakBoostDB() float64 {
	if d.q.Val <= 0.71 {
		return 0
	}
	return 20 * math.Log10(d.q.Val)
}

// ---- Ten Brighter Ideas No. 3 (efficient appliances) ----------------------

const (
	usHouseholds = 110.0   // million U.S. households
	kwhPerHome   = 10700.0 // avg home electricity use, kWh/year
	reactorTWh   = 8.0     // annual output of one ~1 GW reactor, TWh
	gridCO2      = 0.40    // kg CO2 per kWh (U.S. grid average)
	priceKwh     = 0.16    // $ per kWh
)

// bSavedTWh is the annual electricity saved (terawatt-hours) if reduction% of
// each home's use is cut across adoption% of U.S. households.
func (d *Doc) bSavedTWh() float64 {
	homes := usHouseholds * 1e6 * d.adoption.Val / 100
	kwh := homes * kwhPerHome * d.reduction.Val / 100
	return kwh / 1e9
}
func (d *Doc) bReactors() float64 { return d.bSavedTWh() / reactorTWh }
func (d *Doc) bCO2Mt() float64    { return d.bSavedTWh() * gridCO2 } // Mt = 0.4 * TWh
func (d *Doc) bMoneyB() float64   { return d.bSavedTWh() * priceKwh } // $B

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func (d *Doc) Render() {
	kit.FillRect(d.c, d.bounds, kit.WHITE)
	x := margin
	w := d.bounds.Dx() - 2*margin

	switch d.page {
	case 0:
		d.renderProp21(x, w)
	case 1:
		d.renderFilter(x, w)
	default:
		d.renderBrighter(x, w)
	}

	// The reactor info popover floats over the page; drawn last and folded into
	// the content dirty region.
	d.overlayRect = image.Rectangle{}
	if d.info && d.page == 2 {
		d.overlayRect = d.drawReactorInfo()
		d.contentRect = unionRect(d.contentRect, d.overlayRect)
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

// ---- ghosting lab ---------------------------------------------------------

// Sentinel RefreshModes handled specially by the device handler (they don't
// map to a single waveform):
//   - modeVariant: apply the selected ghosting variant to the dynamic regions.
//   - modeFlash:   a full-screen GC16 flash to clear all ghosting.
const (
	modeVariant kit.RefreshMode = 100
	modeFlash   kit.RefreshMode = 101
)

// A redraw variant is a named way to push a changed region to the panel, for
// comparing how each one ghosts. composite 0 = the plain waveform in mode;
// 1 = white-flash then GC16 (de-ghost); 2 = the base waveform in mode with a
// GC16 flash every flashEvery updates. paintMs is roughly how long the update
// takes to finish on the panel — we never issue a scrub update faster than
// this, so updates can't pile up in the controller and drain for seconds after
// you stop.
type redrawVariant struct {
	name       string
	mode       kit.RefreshMode
	composite  int
	paintMs    int
	flashEvery int // composite 2 only: GC16 flash cadence
}

var redrawVariants = []redrawVariant{
	{"A2 pen + flash /10", kit.RefreshPen, 2, 120, 10},
	{"A2 pen (super fast)", kit.RefreshPen, 0, 120, 0},
	{"DU partial", kit.RefreshFast, 0, 230, 0},
	{"DU + flash /8", kit.RefreshFast, 2, 260, 8},
	{"A2 partial", kit.RefreshA2, 0, 120, 0},
	{"DU full", kit.RefreshDUFull, 0, 260, 0},
	{"GC16 partial", kit.RefreshGC16Part, 0, 450, 0},
	{"GC16 flash", kit.RefreshFull, 0, 600, 0},
	{"AUTO partial", kit.RefreshAuto, 0, 300, 0},
	{"white-flash + GC16", kit.RefreshFull, 1, 1100, 0},
}

func (d *Doc) CurrentVariant() redrawVariant { return redrawVariants[d.variant] }

// renderVariantControl draws the tap-to-cycle redraw picker plus a flash
// button; returns the y below it. The left area cycles the variant (and flashes
// clean); the right button flashes on demand.
func (d *Doc) renderVariantControl(x, y, w int) int {
	const h = 56
	const flashW = 168
	kit.Frame(d.c, x, y, w, h, 2, kit.BLACK)

	d.variantHit = image.Rect(x, y, x+w-flashW, y+h)
	d.flashHit = image.Rect(x+w-flashW, y, x+w, y+h)
	// divider between the picker and the flash button
	kit.FillRect(d.c, image.Rect(d.flashHit.Min.X, y+2, d.flashHit.Min.X+2, y+h-2), kit.BLACK)

	by := y + h/2 + kit.Small.Line/2 - 4
	lead := fmt.Sprintf("ghosting lab  [%d/%d]  ", d.variant+1, len(redrawVariants))
	lx := x + 16
	kit.DrawString(d.c, kit.Small, lead, lx, by, kit.GRAY)
	lx += kit.Measure(kit.Small, lead)
	kit.DrawString(d.c, kit.Bold, d.CurrentVariant().name, lx, by, kit.BLACK)

	// flash button
	kit.DrawCentered(d.c, kit.Bold, "FLASH", d.flashHit.Min.X, flashW, by, kit.BLACK)
	return y + h + 14
}

func (d *Doc) renderFilter(x, w int) {
	top := d.header(x, w, "A State-Variable Filter", "Ten Brighter Ideas' cousin  -  slide a bold number")

	y := top
	y = d.flow(x, y, w, []tok{
		st("One input, split three ways at once — low-pass, band-pass, high-pass — around a cutoff of"), vr(d.fc),
		st("with resonance Q of"), vr(d.q),
		st(". Scrub either and watch all three curves reshape."),
	})
	// The full-width strip over the Fc/Q line(s): the only prose that changes
	// during a scrub, so it's all we re-drive up here.
	nbTop := min(d.fc.hit.Min.Y, d.q.hit.Min.Y) - 4
	nbBot := max(d.fc.hit.Max.Y, d.q.hit.Max.Y) + 4
	d.numberBand = image.Rect(x-10, nbTop, x+w+10, nbBot)
	y += kit.Body.Line / 3

	// Ghosting lab: tap-to-cycle picker for how a scrub pushes to the panel.
	y = d.renderVariantControl(x, y, w)

	// Block diagram of the filter: two integrators in a feedback loop. Static —
	// drawn once, never re-driven during a scrub, so it never ghosts.
	diag := image.Rect(x, y, x+w, y+250)
	d.renderSVFDiagram(diag)

	// The three magnitude responses — this is what redraws live as Q is scrubbed.
	d.plotRect = image.Rect(x, diag.Max.Y+16, x+w, diag.Max.Y+16+500)
	d.renderResponse(d.plotRect)

	d.contentRect = image.Rect(x-20, top-10, x+w+20, d.plotRect.Max.Y+20)
}

// ScrubRegions is the set of rectangles that actually change when a value is
// scrubbed on the current page. The filter page returns just the number strip
// and the plot, so the diagram and static prose aren't re-driven (and so can't
// ghost); other pages redraw their whole content area.
func (d *Doc) ScrubRegions() []image.Rectangle {
	if d.page == 1 {
		return []image.Rectangle{d.numberBand, d.plotRect}
	}
	return []image.Rectangle{d.contentRect}
}

// renderSVFDiagram draws the classic state-variable topology: the input is
// summed with feedback, then passed through two integrators; the three taps are
// the high-, band-, and low-pass outputs.
func (d *Doc) renderSVFDiagram(r image.Rectangle) {
	midY := r.Min.Y + 96
	g := kit.BLACK
	// geometry along the signal path
	inX := r.Min.X + 10
	sumX := r.Min.X + 150
	sumR := 28
	box1 := image.Rect(sumX+150, midY-40, sumX+150+120, midY+40)
	box2 := image.Rect(box1.Max.X+150, midY-40, box1.Max.X+150+120, midY+40)
	hpX := (sumX + sumR + box1.Min.X) / 2
	bpX := (box1.Max.X + box2.Min.X) / 2
	lpX := box2.Max.X + 70

	arrow := func(x0, y0, x1, y1 int) {
		kit.Line(d.c, x0, y0, x1, y1, g)
		// small arrowhead at (x1,y1) pointing along +x
		kit.Line(d.c, x1, y1, x1-12, y1-6, g)
		kit.Line(d.c, x1, y1, x1-12, y1+6, g)
	}
	tap := func(tx int, label string) {
		kit.Stamp(d.c, tx, midY, 4, g)
		kit.Line(d.c, tx, midY, tx, midY-56, g)
		kit.Line(d.c, tx, midY-56, tx, midY-56, g)
		kit.Line(d.c, tx, midY-56, tx+10, midY-50, g)
		kit.Line(d.c, tx, midY-56, tx-10, midY-50, g)
		kit.DrawCentered(d.c, kit.H2, label, tx-60, 120, midY-64, g)
	}

	// input
	kit.DrawStringTop(d.c, kit.Small, "in", inX, midY-kit.Small.Asc-2, kit.GRAY)
	arrow(inX+34, midY, sumX-sumR, midY)
	// summing junction
	kit.Frame(d.c, sumX-sumR, midY-sumR, 2*sumR, 2*sumR, 2, g) // (a circle would be nicer; box reads fine)
	kit.DrawCentered(d.c, kit.H2, "+", sumX-sumR, 2*sumR, midY+10, g)
	// summer -> integrator 1 (HP tap in between)
	arrow(sumX+sumR, midY, box1.Min.X, midY)
	tap(hpX, "HP")
	// integrator 1
	kit.Frame(d.c, box1.Min.X, box1.Min.Y, box1.Dx(), box1.Dy(), 2, g)
	kit.DrawCentered(d.c, kit.H2, "1/s", box1.Min.X, box1.Dx(), midY+12, g) // 1/s = an integrator (Laplace)
	// integrator 1 -> integrator 2 (BP tap between)
	arrow(box1.Max.X, midY, box2.Min.X, midY)
	tap(bpX, "BP")
	// integrator 2
	kit.Frame(d.c, box2.Min.X, box2.Min.Y, box2.Dx(), box2.Dy(), 2, g)
	kit.DrawCentered(d.c, kit.H2, "1/s", box2.Min.X, box2.Dx(), midY+12, g)
	// integrator 2 -> LP out
	arrow(box2.Max.X, midY, lpX, midY)
	tap(lpX, "LP")

	// feedback rails back into the summer (drawn below the path)
	railFc := midY + 92 // low-pass feedback sets the cutoff
	railQ := midY + 56  // band-pass feedback sets the damping (1/Q)
	feedback := func(fromX, rail int, label string) {
		kit.Line(d.c, fromX, midY+4, fromX, rail, g)
		kit.Line(d.c, fromX, rail, sumX, rail, g)
		arrow(sumX, rail, sumX, midY+sumR) // up into the summer
		kit.DrawStringTop(d.c, kit.Small, label, (sumX+fromX)/2-20, rail+4, kit.GRAY)
	}
	feedback(bpX, railQ, "damping ~ 1/Q")
	feedback(lpX, railFc, "tune ~ Fc")
}

// renderResponse draws the low-, band-, and high-pass magnitude responses on a
// shared log-frequency axis. Scrubbing Q reshapes all three at once.
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

	// One curve. style: 0 solid, 1 dashed, 2 dotted.
	curve := func(kind, style int, gray uint8) {
		prevY := 0
		for px := 0; px < r.Dx(); px++ {
			t := float64(px) / float64(r.Dx()-1)
			f := fLo * math.Pow(fHi/fLo, t)
			gy := yForDB(svfMagDB(kind, f, d.fc.Val, d.q.Val))
			gx := r.Min.X + px
			if px == 0 {
				prevY = gy
			}
			draw := true
			switch style {
			case 1:
				draw = (px/12)%2 == 0
			case 2:
				draw = px%6 < 2
			}
			if draw {
				if kind == svfLow {
					kit.BrushLine(d.c, gx-1, prevY, gx, gy, 1, gray) // low-pass, bold
				} else {
					kit.Line(d.c, gx-1, prevY, gx, gy, gray)
				}
			}
			prevY = gy
		}
	}
	curve(svfHigh, 2, kit.GRAY)  // high-pass, dotted
	curve(svfBand, 1, kit.BLACK) // band-pass, dashed
	curve(svfLow, 0, kit.BLACK)  // low-pass, solid (drawn last, on top)

	// Legend, top-left inside the plot.
	lx, ly := r.Min.X+70, r.Min.Y+18
	legend := func(row int, style int, gray uint8, label string) {
		yy := ly + row*(kit.Small.Line)
		for sx := 0; sx < 46; sx++ {
			on := true
			switch style {
			case 1:
				on = (sx/12)%2 == 0
			case 2:
				on = sx%6 < 2
			}
			if on {
				kit.PutPx(d.c, lx+sx, yy, gray)
				kit.PutPx(d.c, lx+sx, yy+1, gray)
			}
		}
		kit.DrawStringTop(d.c, kit.Small, label, lx+58, yy-kit.Small.Asc+kit.Small.Line/2-4, kit.GRAY)
	}
	legend(0, 0, kit.BLACK, "low-pass")
	legend(1, 1, kit.BLACK, "band-pass")
	legend(2, 2, kit.GRAY, "high-pass")
}

func (d *Doc) renderBrighter(x, w int) {
	top := d.header(x, w, "Idea No. 3: Efficient Appliances", "Ten Brighter Ideas  -  slide a bold number")

	y := top
	y = d.flow(x, y, w, []tok{
		st("Swapping heating, lighting, cooling, and appliances for efficient models can cut a home's electricity use by"),
		vr(d.reduction),
		st(". If"), vr(d.adoption),
		st("of America's 110 million households did it, each year the country would save:"),
	})
	y += kit.Body.Line / 2

	// Three stat bars — energy, CO2, money — scaled to sensible full-scales.
	saved := d.bSavedTWh()
	barX, barW := x, w
	rowH := 96
	d.statBar(barX, y, barW, rowH, "electricity", fmt.Sprintf("%.0f TWh", saved), saved/600)
	y += rowH
	d.statBar(barX, y, barW, rowH, "carbon", fmt.Sprintf("%.0f Mt CO2", d.bCO2Mt()), d.bCO2Mt()/240)
	y += rowH
	d.statBar(barX, y, barW, rowH, "money", fmt.Sprintf("$%.0f billion", d.bMoneyB()), d.bMoneyB()/96)
	y += rowH + kit.Body.Line/2

	// Pictograph: reactors displaced, one cooling tower per reactor. The whole
	// stat (phrase + grid) is a hover target for the context popover.
	n := int(math.Round(d.bReactors()))
	phraseY := y
	y = d.flow(x, y, w, []tok{
		st("That's like shutting down"), bd(fmt.Sprintf("%d", n)),
		st("nuclear reactors  (hover for context):"),
	})
	y += 20
	y = d.reactorGrid(x, y, w, n)
	d.reactorHit = image.Rect(x-10, phraseY-6, x+w+10, y+6)

	d.contentRect = image.Rect(x-20, top-10, x+w+20, y+20)
}

// statBar draws a labelled horizontal bar. frac (0..1+) sets the fill; the
// value string sits at the right.
func (d *Doc) statBar(x, y, w, h int, label, value string, frac float64) {
	kit.DrawStringTop(d.c, kit.Small, label, x, y, kit.GRAY)
	track := image.Rect(x, y+kit.Small.Line+2, x+w, y+kit.Small.Line+2+40)
	kit.Frame(d.c, track.Min.X, track.Min.Y, track.Dx(), track.Dy(), 2, kit.BLACK)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	fillW := int(float64(track.Dx()-6) * frac)
	kit.FillRect(d.c, image.Rect(track.Min.X+3, track.Min.Y+3, track.Min.X+3+fillW, track.Max.Y-3), kit.BLACK)
	// value, right-aligned, in the bold body face just above the track
	vw := kit.Measure(kit.Bold, value)
	kit.DrawString(d.c, kit.Bold, value, x+w-vw, y+kit.Small.Asc, kit.BLACK)
}

// reactorGrid lays out n cooling-tower icons in a wrapping row. Returns the y
// below the grid.
func (d *Doc) reactorGrid(x, y, w, n int) int {
	const iw, ih, gap = 44, 62, 20
	perRow := (w + gap) / (iw + gap)
	if perRow < 1 {
		perRow = 1
	}
	cx, cy := x, y
	for i := 0; i < n; i++ {
		if i > 0 && i%perRow == 0 {
			cx = x
			cy += ih + gap
		}
		coolingTower(d.c, cx, cy, iw, ih)
		cx += iw + gap
	}
	rows := (n + perRow - 1) / perRow
	if rows < 1 {
		rows = 1
	}
	return y + rows*(ih+gap)
}

// coolingTower draws a simple hyperbolic-tower silhouette (a pinched trapezoid)
// with a little steam puff — recognisable as a reactor at icon size.
func coolingTower(c *image.RGBA, x, y, w, h int) {
	waist := w * 55 / 100 // narrowest width, near the top third
	topY := y + h/4
	for row := topY; row < y+h; row++ {
		// linear taper from waist (at topY) to full width (at base)
		t := float64(row-topY) / float64(y+h-topY)
		ww := int(float64(waist) + t*float64(w-waist))
		x0 := x + (w-ww)/2
		kit.FillRect(c, image.Rect(x0, row, x0+ww, row+1), kit.BLACK)
	}
	// flared rim
	kit.FillRect(c, image.Rect(x+(w-waist)/2-3, topY, x+(w+waist)/2+3, topY+3), kit.BLACK)
	// steam
	kit.Stamp(c, x+w/2, topY-10, 6, kit.GRAY)
	kit.Stamp(c, x+w/2-9, topY-16, 5, kit.LGRAY)
	kit.Stamp(c, x+w/2+9, topY-16, 5, kit.LGRAY)
}

// Per-reactor figures, from Bret Victor's Ten Brighter Ideas (1.5 reactors =
// $494M/yr and 32.7 t/yr of waste, out of 104 U.S. reactors).
const (
	usReactors     = 104.0
	reactorCostB   = 0.329 // $ billion / reactor / year to run
	reactorWasteT  = 21.8  // tonnes of radioactive waste / reactor / year
)

// drawReactorInfo reveals a context panel below the reactor pictograph — what
// those displaced reactors mean in share, cost, and waste. Reactive: it tracks
// the scrubbed values. Returns its rect (for the dirty region).
func (d *Doc) drawReactorInfo() image.Rectangle {
	n := math.Round(d.bReactors())
	x := margin
	w := d.bounds.Dx() - 2*margin
	py := d.reactorHit.Max.Y + 16
	ph := kit.H2.Line + 3*kit.Body.Line + 40
	if py+ph > d.bounds.Dy()-110 { // keep clear of the footer
		py = d.bounds.Dy() - 110 - ph
	}
	r := image.Rect(x, py, x+w, py+ph)

	kit.FillRect(d.c, r, kit.WHITE)
	kit.Frame(d.c, r.Min.X, r.Min.Y, r.Dx(), r.Dy(), 3, kit.BLACK)

	ix, iy := x+24, py+18
	kit.DrawStringTop(d.c, kit.H2, "Those reactors, in context", ix, iy, kit.BLACK)
	iy += kit.H2.Line + 8
	pct := 0.0
	if n > 0 {
		pct = n / usReactors * 100
	}
	lines := []string{
		fmt.Sprintf("= %.1f%% of the 104 reactors in the U.S. fleet", pct),
		fmt.Sprintf("~ $%.1f billion a year no longer spent running them", n*reactorCostB),
		fmt.Sprintf("~ %.0f tonnes a year of radioactive waste never made", n*reactorWasteT),
	}
	for _, s := range lines {
		kit.DrawStringTop(d.c, kit.Body, s, ix, iy, kit.GRAY)
		iy += kit.Body.Line
	}
	return r
}

func unionRect(a, b image.Rectangle) image.Rectangle {
	if a.Empty() {
		return b
	}
	if b.Empty() {
		return a
	}
	return a.Union(b)
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
		// Closing punctuation attaches to the previous word (no leading space),
		// so a scrubbable value followed by ". " reads "2.0." not "2.0 .".
		attach := len(wd.s) > 0 && strings.ContainsRune(".,;:!?)%", rune(wd.s[0]))
		if cx > x {
			sp := space
			if attach {
				sp = 0
			}
			if cx+sp+ww > x+maxW {
				cx = x
				baseline += kit.Body.Line
			} else {
				cx += sp
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

// HandleTouch feeds one event through the interaction model and returns the
// region to refresh, the waveform, and whether to push to the panel now.
//
// Scrubbing is LAZY: each move only updates the value (cheap) and marks
// needRender; the expensive Render happens at the throttle window or on
// release, so a fast drag jumps straight to the final value instead of
// grinding through every intermediate. modeVariant means "apply the selected
// ghosting variant" (filter page only).
func (d *Doc) HandleTouch(t kit.Touch) (image.Rectangle, kit.RefreshMode, bool) {
	d.penX, d.penY = t.X, t.Y

	// Ghosting-lab controls (filter page), tapped with a plain touch (no button).
	if t.Kind == kit.TouchDown && !t.Button && d.page == 1 {
		if image.Pt(t.X, t.Y).In(d.flashHit) {
			return d.bounds, modeFlash, true // manual full-screen de-ghost
		}
		if image.Pt(t.X, t.Y).In(d.variantHit) {
			d.variant = (d.variant + 1) % len(redrawVariants)
			d.Render()
			return d.bounds, modeFlash, true // flash clean on every mode change
		}
	}

	// Begin scrubbing: pen button pressed over a bold number.
	if t.Button && d.scrub == nil {
		if v := d.varAt(t.X, t.Y); v != nil {
			d.scrub = v
			d.scrubStartX = t.X
			d.scrubStart = v.Val
			d.hover = v
			d.needRender = true
			return d.flushScrub(true, false)
		}
	}

	// Continue scrubbing: ONLY update the value here — never render. Rendering
	// is deferred to Tick, which runs once per main-loop iteration AFTER the
	// whole input backlog has been drained. So a fast drag that queues 50 move
	// events collapses to a single render of the latest value, instead of
	// replaying every intermediate step (that replay is what kept the panel
	// updating for seconds after you let go).
	if d.scrub != nil && t.Button {
		if d.scrub.setFromScrub(d.scrubStart, t.X-d.scrubStartX) {
			d.needRender = true
		}
		return empty()
	}

	// End scrubbing: jump to the final value and render it once.
	if d.scrub != nil && !t.Button {
		d.scrub = nil
		d.hover = d.varAt(t.X, t.Y)
		d.needRender = true
		return d.flushScrub(true, true)
	}

	// Reactor info popover: hovering the reactor stat toggles it (Brighter).
	if d.page == 2 {
		if over := image.Pt(t.X, t.Y).In(d.reactorHit); over != d.info {
			d.info = over
			prevOverlay := d.overlayRect
			d.Render()
			return unionRect(unionRect(d.reactorHit, prevOverlay), d.overlayRect), kit.RefreshFast, true
		}
	}

	// Plain hover: move the underline to whatever number the pen is over.
	if nh := d.varAt(t.X, t.Y); nh != d.hover {
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
			return dirty.Rect(d.bounds), kit.RefreshFast, true
		}
	}
	return empty()
}

// scrubInterval is the minimum gap between scrub updates — set to how long the
// current redraw actually takes to paint, so we issue at (not above) the panel's
// sustainable rate. That, plus rendering only the latest value, is the whole
// fix for the runaway update cycle: at most one paint is ever "in flight", and
// the next one always goes to wherever the pen is NOW.
func (d *Doc) scrubInterval() time.Duration {
	if d.page == 1 {
		return time.Duration(d.CurrentVariant().paintMs) * time.Millisecond
	}
	return refreshEvery
}

// flushScrub renders the LATEST scrubbed value and returns its refresh — but
// only if forced (scrub start/end) or the pacing window has elapsed.
// Intermediate values that fall between windows are never rendered.
func (d *Doc) flushScrub(force, settle bool) (image.Rectangle, kit.RefreshMode, bool) {
	if !d.needRender {
		return empty()
	}
	if !force && time.Since(d.lastRefresh) < d.scrubInterval() {
		return empty()
	}
	d.Render()
	d.needRender = false
	d.lastRefresh = time.Now()
	d.pendingMode = d.scrubMode(settle)
	return d.contentRect, d.pendingMode, true
}

// scrubMode picks the waveform for a scrub refresh. The filter page defers to
// the ghosting lab (so you can see how each variant ghosts, including on the
// settling pass); other pages use fast DU while dragging and a clean AUTO
// settle on release.
func (d *Doc) scrubMode(settle bool) kit.RefreshMode {
	if d.page == 1 {
		return modeVariant
	}
	if settle {
		return kit.RefreshAuto
	}
	return kit.RefreshFast
}

// Tick renders a throttled scrub change once the window has elapsed, so a drag
// that stops mid-slide (button still held) still settles without a new event.
func (d *Doc) Tick() (image.Rectangle, kit.RefreshMode, bool) {
	return d.flushScrub(false, false)
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
