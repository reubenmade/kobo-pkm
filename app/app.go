package main

import (
	"image"
	"log"
	"time"
)

// RefreshMode maps to e-ink waveforms: Fast = DU (binary, low latency,
// for pen strokes and selection), Auto = controller picks (widget updates),
// Full = GC16 with a full flash (page turns, clears ghosting).
type RefreshMode int

const (
	RefreshFast RefreshMode = iota
	RefreshAuto
	RefreshFull
	RefreshPen // lowest latency: A2 pen waveform on MTK, DU elsewhere
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
)

type Touch struct {
	Kind TouchKind
	X, Y int
}

type Screen interface {
	Render(a *App) // draw full screen onto the canvas (no refresh)
	Touch(a *App, t Touch)
}

// Modal is drawn over the current screen and receives all touches while open.
type Modal interface {
	Render(a *App)
	Touch(a *App, t Touch)
}

type App struct {
	D       Display
	W, H    int
	Touches chan Touch
	Keys    chan int // physical button key codes
	Store   *Store
	stack   []Screen
	modal   Modal
	Quit    bool
}

// KeyScreen is implemented by screens that react to physical buttons.
type KeyScreen interface {
	Key(a *App, code int)
}

func NewApp(d Display, store *Store) *App {
	b := d.Bounds()
	return &App{
		D:       d,
		W:       b.Dx(),
		H:       b.Dy(),
		Touches: make(chan Touch, 64),
		Keys:    make(chan int, 8),
		Store:   store,
	}
}

func (a *App) top() Screen {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1]
}

func (a *App) Push(s Screen) {
	log.Printf("ui: push %T", s)
	a.stack = append(a.stack, s)
	a.Render(RefreshFull)
}

func (a *App) Pop() {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
	log.Printf("ui: pop -> %T", a.top())
	a.Render(RefreshFull)
}

// Render redraws the top screen (and modal, if any) and refreshes the display.
func (a *App) Render(mode RefreshMode) {
	FillRect(a.D.Canvas(), a.D.Bounds(), 255)
	if s := a.top(); s != nil {
		s.Render(a)
	}
	if a.modal != nil {
		a.modal.Render(a)
	}
	a.D.Refresh(a.D.Bounds(), mode)
}

// sizedModal lets a modal declare its footprint so opening it can flash
// just that region with the full waveform — kills e-ink ghosting under it.
type sizedModal interface{ Rect() image.Rectangle }

func (a *App) ShowModal(m Modal) {
	a.modal = m
	a.Render(RefreshAuto)
	if sm, ok := m.(sizedModal); ok {
		a.D.Refresh(sm.Rect().Inset(-10), RefreshFull)
	}
}

func (a *App) CloseModal() {
	a.modal = nil
	a.Render(RefreshAuto)
}

// Dispatch routes one touch to the modal (if open) or the top screen.
func (a *App) Dispatch(t Touch) {
	if a.modal == nil && t.Kind == TouchUp && HandleStatusBarTap(a, t) {
		return
	}
	if a.modal != nil {
		a.modal.Touch(a, t)
	} else if s := a.top(); s != nil {
		s.Touch(a, t)
	}
}

func (a *App) Run() {
	a.Render(RefreshFull)
	a.RunNoInitialRender()
}

// RunNoInitialRender is Run for callers that already painted the first screen.
func (a *App) RunNoInitialRender() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for !a.Quit {
		select {
		case t, ok := <-a.Touches:
			if !ok {
				return
			}
			a.Dispatch(t)
		case code := <-a.Keys:
			if a.modal == nil {
				if h, ok := a.top().(KeyScreen); ok {
					h.Key(a, code)
				}
			}
		case <-tick.C:
			// Keep the status bar clock/battery fresh without disturbing
			// whatever the user is doing.
			if a.modal == nil && a.top() != nil {
				a.top().Render(a)
				a.D.Refresh(image.Rect(0, 0, a.W, StatusBarH), RefreshAuto)
			}
		}
	}
	log.Printf("app: quit requested")
}
