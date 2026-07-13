//go:build sim

package main

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// SimDisplay renders to PNG files instead of a framebuffer so the whole
// app can be exercised on a dev machine.
type SimDisplay struct {
	mu     sync.Mutex
	canvas *image.RGBA
	dir    string
	n      int
}

func NewSimDisplay(dir string, w, h int) *SimDisplay {
	os.MkdirAll(dir, 0755)
	return &SimDisplay{canvas: image.NewRGBA(image.Rect(0, 0, w, h)), dir: dir}
}

func (d *SimDisplay) Bounds() image.Rectangle { return d.canvas.Bounds() }
func (d *SimDisplay) Canvas() *image.RGBA     { return d.canvas }
func (d *SimDisplay) Close()                  {}

func (d *SimDisplay) Refresh(r image.Rectangle, mode RefreshMode) {
	// no-op: snapshots are taken explicitly by the scenario
}

func (d *SimDisplay) Snap(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.n++
	path := filepath.Join(d.dir, fmt.Sprintf("%02d-%s.png", d.n, name))
	f, err := os.Create(path)
	if err != nil {
		log.Fatalf("snap: %v", err)
	}
	png.Encode(f, d.canvas)
	f.Close()
	log.Printf("snap: %s", path)
}
