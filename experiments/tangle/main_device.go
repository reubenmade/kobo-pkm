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
	rt *kit.Runtime
	d  *Doc
}

func (h *handler) Start() {
	h.d.SetPage(0)
	h.rt.RefreshAll(kit.RefreshFull)
}

func (h *handler) Touch(t kit.Touch) {
	if r, mode, ok := h.d.HandleTouch(t); ok {
		h.rt.Refresh(r, mode)
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
		h.rt.Refresh(r, mode)
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
