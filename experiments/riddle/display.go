package main

import "image"

// RefreshMode maps to e-ink waveforms: Fast = DU (binary, low latency),
// Auto = controller picks, Full = GC16 with a full flash (clears ghosting),
// Pen = A2 + FORCE_A2_OUTPUT on the hwtcon driver — the instant-ink path.
type RefreshMode int

const (
	RefreshFast RefreshMode = iota
	RefreshAuto
	RefreshFull
	RefreshPen
)

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
	// TouchHover: the pen is in range but not touching — position only.
	// The elan reports the pen while it hovers; contact is pressure-gated.
	TouchHover
)

// Touch is one digitizer event, already mapped to screen coordinates. On the
// Kobo the pen and fingers arrive through the same elan touchscreen; Pressure
// is 0 when the digitizer doesn't report it, and Eraser only fires if the
// hardware ever sends BTN_TOOL_RUBBER (logged so we can find out).
type Touch struct {
	Kind     TouchKind
	X, Y     int
	Pressure int
	Eraser   bool
	Button   bool // pen side button held
}
