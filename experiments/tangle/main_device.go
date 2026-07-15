//go:build linux && !sim

package main

import (
	"image"
	"os"
	"path/filepath"

	"github.com/reubenmade/kobo-pkm/kit"
)

// Tangle on the Kobo. Launched by run.sh with Nickel dead (takeover). The kit
// owns the whole lifecycle — this file only wires the reactive document into
// the Handler interface.

type handler struct {
	rt     *kit.Runtime
	d      *Doc
	flashN int // for the "GC16 flash every 8th" ghosting variant
}

func (h *handler) Start() {
	h.d.SetPage(0)
	h.rt.RefreshAll(kit.RefreshFull)
}

func (h *handler) Touch(t kit.Touch) {
	if r, mode, ok := h.d.HandleTouch(t); ok {
		h.push(r, mode)
	}
}

func (h *handler) Key(code int) {
	switch code {
	case kit.KeyPageForward:
		h.d.NextPage()
		h.rt.RefreshAll(kit.RefreshFull)
	case kit.KeyPageBack:
		h.d.PrevPage()
		h.rt.RefreshAll(kit.RefreshFull)
	}
}

func (h *handler) Step() {
	if r, mode, ok := h.d.Tick(); ok {
		h.push(r, mode)
	}
}

// push sends a changed region to the panel.
//   - modeFlash:   a full-screen GC16 flash (clears all ghosting).
//   - modeVariant: run the selected ghosting variant over ONLY the regions that
//     changed (number strip + plot on the filter page), so the static prose and
//     diagram are never re-driven and can't ghost.
//   - anything else: a plain refresh of the given region.
func (h *handler) push(r image.Rectangle, mode kit.RefreshMode) {
	switch mode {
	case modeFlash:
		h.rt.RefreshAll(kit.RefreshFull)
	case modeVariant:
		h.applyVariant()
	default:
		h.rt.Refresh(r, mode)
	}
}

func (h *handler) applyVariant() {
	v := h.d.CurrentVariant()
	regions := h.d.ScrubRegions()
	switch v.composite {
	case 1: // white-flash then GC16: clear each region, then repaint clean
		for _, reg := range regions {
			kit.FillRect(h.rt.Canvas(), reg, kit.WHITE)
			h.rt.Refresh(reg, kit.RefreshFull)
		}
		h.d.Render()
		for _, reg := range regions {
			h.rt.Refresh(reg, kit.RefreshFull)
		}
	case 2: // base waveform, with a GC16 flash every flashEvery updates
		h.flashN++
		flash := v.flashEvery > 0 && h.flashN%v.flashEvery == 0
		for _, reg := range regions {
			if flash {
				h.rt.Refresh(reg, kit.RefreshFull)
			} else {
				h.rt.Refresh(reg, v.mode)
			}
		}
	default:
		for _, reg := range regions {
			h.rt.Refresh(reg, v.mode)
		}
	}
}

func (h *handler) SleepScreen(c *image.RGBA) {
	DrawSplash(c, h.rt.Bounds(), "Tangle sleeps", "press power or open the cover to wake")
}

func (h *handler) ExitScreen(c *image.RGBA) {
	DrawSplash(c, h.rt.Bounds(), "Tangle closed", "the Kobo home returns in a moment")
}

func main() {
	base := "/mnt/onboard/.adds/tangle"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	cfg := kit.LoadConfig(filepath.Join(base, "config.txt"))
	kit.Run(cfg, base, func(rt *kit.Runtime) kit.Handler {
		return &handler{rt: rt, d: NewDoc(rt.Canvas(), rt.Bounds())}
	})
}
