// Package kit is the shared device + drawing layer for kobo-pkm native apps.
// It is the best-of extraction from app/ and experiments/riddle: framebuffer,
// evdev pen/touch/keys, the MTK suspend ritual, mono drawing primitives, pen
// ink capture, legible text layout, config, a PNG simulator backend, and an
// App runner that owns the whole Nickel-takeover main loop.
//
// A new experiment implements Handler and calls Run; everything else is here.
package kit

import "image"

// RefreshMode maps to an e-ink waveform + update-mode (partial/full). The
// waveform decides speed vs. ghosting; a "full" update flashes the whole region
// to clear ghosting, a "partial" one doesn't.
//
//   - Fast     = DU,   partial — binary, low latency, ghosts build up.
//   - Auto     = AUTO, partial — the controller picks per region.
//   - Full     = GC16, full    — 16-grey flash, cleanest, slowest.
//   - Pen      = A2 + FORCE_A2_OUTPUT, partial — the instant-ink path, heaviest ghost.
//   - A2       = A2,   partial — 2-level fast, no force flag.
//   - DUFull   = DU,   full    — binary but flashed full.
//   - GC16Part = GC16, partial — 16-grey, no flash (clean but leaves faint edges).
//   - AutoFull = AUTO, full.
//
// The one rule that matters: never issue a Refresh per input event. Draw into
// the canvas freely, Refresh a bounded rect at most ~16 Hz, and do one settling
// pass when a gesture ends.
type RefreshMode int

const (
	RefreshFast RefreshMode = iota
	RefreshAuto
	RefreshFull
	RefreshPen
	RefreshA2
	RefreshDUFull
	RefreshGC16Part
	RefreshAutoFull
)

// Display is the render target — a framebuffer on device, PNG files in the sim.
type Display interface {
	Bounds() image.Rectangle
	Canvas() *image.RGBA
	Refresh(r image.Rectangle, mode RefreshMode)
	Close()
}

type TouchKind int

const (
	TouchDown TouchKind = iota
	TouchMove
	TouchUp
	// TouchHover: the pen is in range but not in contact — position only. The
	// elan streams the pen the whole time it hovers; contact is pressure-gated
	// (see input_linux.go), so hover is a free "where is the pen" channel.
	TouchHover
)

// Touch is one digitizer event, already mapped to screen coordinates. Pen and
// fingers arrive through the same elan touchscreen. Pressure is 0..4096 (0 when
// the device reports none). Button is the pen's side button (BTN_STYLUS2) —
// the gesture modifier the Tangle experiment scrubs with. Eraser is the pen's
// tail (BTN_STYLUS).
type Touch struct {
	Kind     TouchKind
	X, Y     int
	Pressure int
	Eraser   bool
	Button   bool
}
