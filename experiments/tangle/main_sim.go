//go:build sim

package main

import (
	"log"
	"os"

	"github.com/reubenmade/kobo-pkm/kit"
)

// Simulator: drives the reactive document against a PNG display so every state
// — each page, a hover, and a full pen-button scrub of each number — can be
// eyeballed without a Kobo. Build: go build -tags sim -o build/tangle-sim .
// Run:   ./build/tangle-sim simout   (writes NN-name.png snapshots).

func main() {
	out := "simout"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	os.RemoveAll(out)
	kit.InitFonts()

	disp := kit.NewSimDisplay(out, 1264, 1680) // Libra Colour panel
	d := NewDoc(disp.Canvas(), disp.Bounds())

	// -- Page 0: Proposition 21 -----------------------------------------
	d.SetPage(0)
	disp.Snap("prop21-default")

	hover(d, d.surcharge)
	disp.Snap("prop21-hover-surcharge")

	scrub(d, d.surcharge, 30)
	disp.Snap("prop21-surcharge-30")

	scrub(d, d.vehicles, 34)
	disp.Snap("prop21-vehicles-34")

	scrub(d, d.surcharge, 4) // budget goes negative — leftover shows a shortfall
	disp.Snap("prop21-surcharge-low")

	// -- Page 1: the state-variable filter ------------------------------
	d.SetPage(1)
	disp.Snap("filter-default")

	scrub(d, d.q, 8) // tall resonant peak
	disp.Snap("filter-q-8")

	scrub(d, d.fc, 4000) // corner slides right
	disp.Snap("filter-fc-4000")

	scrub(d, d.q, 0.7) // flat (Butterworth), no peak
	disp.Snap("filter-q-flat")

	log.Printf("sim: done — %d snapshots in %s", disp.Count(), out)
}

// hover moves the pen over a var (button up) so its underline shows.
func hover(d *Doc, v *Var) {
	cx, cy := center(v)
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx, Y: cy})
}

// scrub performs a full pen-button-held slide of v to target, in small steps,
// exactly as the device would stream hover-with-button events.
func scrub(d *Doc, v *Var, target float64) {
	cx, cy := center(v)
	// press the button while over the number
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx, Y: cy, Button: true})
	dx := int((target - v.Val) / v.Step * v.PxPerStep)
	const n = 24
	for i := 1; i <= n; i++ {
		d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx + dx*i/n, Y: cy, Button: true})
	}
	// release
	d.HandleTouch(kit.Touch{Kind: kit.TouchHover, X: cx + dx, Y: cy, Button: false})
}

func center(v *Var) (int, int) {
	return (v.hit.Min.X + v.hit.Max.X) / 2, (v.hit.Min.Y + v.hit.Max.Y) / 2
}
