//go:build sim

package main

import (
	"image"
	"log"
	"os"

	"github.com/reubenmade/kobo-pkm/kit"
)

// Simulator: drives the reactive document against a PNG display so every state
// can be eyeballed without a Kobo. Build: go build -tags sim -o build/tangle-sim .
// Run:   ./build/tangle-sim simout   (writes NN-name.png snapshots).
//
// Rendering is lazy on device (throttled), so the sim forces a Render before
// each snapshot via snap().

func main() {
	out := "simout"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	os.RemoveAll(out)
	kit.InitFonts()

	disp := kit.NewSimDisplay(out, 1264, 1680) // Libra Colour panel
	d := NewDoc(disp.Canvas(), disp.Bounds())
	snap := func(name string) { d.Render(); disp.Snap(name) }

	// -- Page 0: Proposition 21 -----------------------------------------
	d.SetPage(0)
	snap("prop21-default")
	hover(d, d.surcharge)
	snap("prop21-hover-surcharge")
	scrub(d, d.surcharge, 30)
	snap("prop21-surcharge-30")
	scrub(d, d.vehicles, 34)
	snap("prop21-vehicles-34")
	scrub(d, d.surcharge, 4) // budget dips — leftover shows a shortfall
	snap("prop21-surcharge-low")

	// -- Page 1: the state-variable filter + ghosting lab ---------------
	d.SetPage(1)
	snap("filter-default")
	scrub(d, d.q, 8) // tall resonant peak on low/high-pass
	snap("filter-q-8")
	scrub(d, d.fc, 4000) // corner slides right
	snap("filter-fc-4000")
	scrub(d, d.q, 0.7) // flat (Butterworth), no peak
	snap("filter-q-flat")
	// cycle the ghosting variant a few times
	tap(d, d.variantHit)
	tap(d, d.variantHit)
	snap("filter-variant-3")

	// -- Page 2: Ten Brighter Ideas No. 3 -------------------------------
	d.SetPage(2)
	snap("brighter-default")
	scrub(d, d.reduction, 35)
	snap("brighter-reduction-35")
	scrub(d, d.adoption, 95)
	snap("brighter-adoption-95")
	// hover the reactor stat to reveal the context popover
	hoverAt(d, d.reactorHit.Min.X+40, (d.reactorHit.Min.Y+d.reactorHit.Max.Y)/2)
	snap("brighter-reactor-info")
	hoverAt(d, 10, 10) // move off the stat — popover hides
	scrub(d, d.reduction, 5)
	snap("brighter-low")

	log.Printf("sim: done — %d snapshots in %s", disp.Count(), out)
}

// hover moves the pen over a var (button up) so its underline shows.
func hover(d *Doc, v *Var) {
	cx, cy := center(v)
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx, Y: cy})
}

func hoverAt(d *Doc, x, y int) {
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: x, Y: y})
}

// tap simulates a finger/pen tap (down+up, no side button) at a rect's centre —
// used to cycle the ghosting-lab control.
func tap(d *Doc, r image.Rectangle) {
	x, y := (r.Min.X+r.Max.X)/2, (r.Min.Y+r.Max.Y)/2
	d.HandleTouch(kit.Touch{Kind: kit.TouchDown, X: x, Y: y})
	d.HandleTouch(kit.Touch{Kind: kit.TouchUp, X: x, Y: y})
}

// scrub performs a full pen-button-held slide of v to target, in small steps,
// exactly as the device streams hover-with-button events, then releases.
func scrub(d *Doc, v *Var, target float64) {
	cx, cy := center(v)
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx, Y: cy, Button: true})
	dx := int((target - v.Val) / v.Step * v.PxPerStep)
	const n = 24
	for i := 1; i <= n; i++ {
		d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx + dx*i/n, Y: cy, Button: true})
	}
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx + dx, Y: cy, Button: false})
}

func center(v *Var) (int, int) {
	return (v.hit.Min.X + v.hit.Max.X) / 2, (v.hit.Min.Y + v.hit.Max.Y) / 2
}
