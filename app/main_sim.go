//go:build sim

package main

import (
	"log"
	"os"
)

// Simulator: runs the real app logic against a PNG display and a scripted
// touch scenario, so screens and interactions can be verified off-device.

func main() {
	out := "simout"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	InitFonts()
	d := NewSimDisplay(out, 1264, 1680) // Libra 2 panel
	os.RemoveAll(out + "/data")
	store := OpenStore(out + "/data")
	a := NewApp(d, store)
	a.stack = append(a.stack, NewDashboard())
	a.Render(RefreshFull)

	tap := func(x, y int) {
		a.Dispatch(Touch{TouchDown, x, y})
		a.Dispatch(Touch{TouchUp, x, y})
	}
	drag := func(x0, y0, x1, y1, steps int) {
		a.Dispatch(Touch{TouchDown, x0, y0})
		for i := 1; i <= steps; i++ {
			a.Dispatch(Touch{TouchMove, x0 + (x1-x0)*i/steps, y0 + (y1-y0)*i/steps})
		}
		a.Dispatch(Touch{TouchUp, x1, y1})
	}

	d.Snap("dashboard")

	// toggle a todo
	tap(300, 660)
	d.Snap("dashboard-todo")

	// dashboard -> index
	tap(1024, 1570)
	d.Snap("index")

	// open first article
	tap(632, 250)
	d.Snap("article-p1")

	// page forward and back
	tap(1100, 800)
	d.Snap("article-p2")
	tap(150, 800)

	// drag-select some words -> colour popover
	drag(200, 420, 950, 470, 8)
	d.Snap("popover")
	tap(466, 703) // "+ note" -> pen pad
	d.Snap("penpad")
	drag(120, 950, 500, 1100, 12)
	drag(500, 950, 150, 1100, 12)
	d.Snap("penpad-drawn")
	tap(527, 1605) // "dn" scroll
	drag(200, 900, 700, 1000, 10)
	d.Snap("penpad-scrolled")
	tap(320, 1605)  // "up"
	tap(1148, 1605) // Save
	d.Snap("article-note-underline")

	// second selection: tap a swatch -> instant colour highlight
	drag(200, 560, 800, 610, 8)
	d.Snap("popover-2")
	tap(429, 729) // green swatch (instant save)
	d.Snap("article-colour")

	// tap the noted words -> pen editor opens with existing strokes
	tap(400, 440)
	d.Snap("note-editor")
	tap(941, 1605) // Cancel
	d.Snap("final")

	log.Printf("sim: done, %d highlights stored", len(store.Highlights))
}
