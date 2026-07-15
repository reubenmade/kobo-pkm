//go:build sim

package kit

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

// SimDisplay renders to PNG files instead of a framebuffer, so an experiment's
// whole state machine can be exercised (and eyeballed) on a dev machine. Build
// with -tags sim. This is the highest-leverage tool in the repo — build every
// state here before touching hardware.
type SimDisplay struct {
	canvas *image.RGBA
	dir    string
	n      int
}

func NewSimDisplay(dir string, w, h int) *SimDisplay {
	os.MkdirAll(dir, 0o755)
	return &SimDisplay{canvas: image.NewRGBA(image.Rect(0, 0, w, h)), dir: dir}
}

func (d *SimDisplay) Bounds() image.Rectangle { return d.canvas.Bounds() }
func (d *SimDisplay) Canvas() *image.RGBA     { return d.canvas }
func (d *SimDisplay) Close()                  {}

// Refresh is a no-op; the scenario takes snapshots explicitly via Snap.
func (d *SimDisplay) Refresh(r image.Rectangle, mode RefreshMode) {}

// Snap writes the current canvas to NN-name.png in the output dir.
func (d *SimDisplay) Snap(name string) {
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

// Count returns how many snapshots have been taken.
func (d *SimDisplay) Count() int { return d.n }
