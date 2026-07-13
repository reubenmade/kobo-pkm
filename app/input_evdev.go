//go:build linux && !sim

package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Touch input straight from evdev. The device is grabbed (EVIOCGRAB) so
// Nickel doesn't also react to our touches underneath the app.

const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport        = 0
	btnTouch         = 0x14a
	absX             = 0x00
	absY             = 0x01
	absPressure      = 0x18
	absMTPositionX   = 0x35
	absMTPositionY   = 0x36
	absMTTrackingID  = 0x39
	eviocgrab        = 0x40044590
)

type absInfo struct {
	Value, Min, Max, Fuzz, Flat, Res int32
}

func eviocgabs(code uint16) uintptr {
	// _IOR('E', 0x40+code, struct input_absinfo[24])
	return uintptr(2<<30 | 24<<16 | 0x45<<8 | (0x40 + uint32(code)))
}

func eviocgname(l int) uintptr {
	return uintptr(2<<30 | uint32(l)<<16 | 0x45<<8 | 0x06)
}

type TouchReader struct {
	f      *os.File
	maxX, maxY int
	cfg    Config
	W, H   int
	logged int
}

func findTouchDevice(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	matches, _ := filepath.Glob("/dev/input/event*")
	best := ""
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		name := make([]byte, 256)
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgname(256), uintptr(unsafe.Pointer(&name[0])))
		f.Close()
		n := strings.ToLower(strings.TrimRight(string(name), "\x00"))
		log.Printf("input: %s = %q", path, n)
		if strings.Contains(n, "touch") || strings.Contains(n, "elan") ||
			strings.Contains(n, "cyttsp") || strings.Contains(n, "pixcir") ||
			strings.Contains(n, "ekth") || strings.Contains(n, "zforce") {
			best = path
		}
	}
	if best == "" {
		best = "/dev/input/event1" // historical Kobo default
		log.Printf("input: no obvious touch device, defaulting to %s", best)
	}
	return best, nil
}

func OpenTouch(cfg Config, w, h int) (*TouchReader, error) {
	path, err := findTouchDevice(cfg.TouchDev)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	t := &TouchReader{f: f, cfg: cfg, W: w, H: h, maxX: 0, maxY: 0}
	var ai absInfo
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgabs(absMTPositionX), uintptr(unsafe.Pointer(&ai))); e == 0 && ai.Max > 0 {
		t.maxX = int(ai.Max)
	}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgabs(absMTPositionY), uintptr(unsafe.Pointer(&ai))); e == 0 && ai.Max > 0 {
		t.maxY = int(ai.Max)
	}
	if t.maxX == 0 {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgabs(absX), uintptr(unsafe.Pointer(&ai))); e == 0 && ai.Max > 0 {
			t.maxX = int(ai.Max)
		}
	}
	if t.maxY == 0 {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgabs(absY), uintptr(unsafe.Pointer(&ai))); e == 0 && ai.Max > 0 {
			t.maxY = int(ai.Max)
		}
	}
	if t.maxX == 0 {
		t.maxX = w - 1
	}
	if t.maxY == 0 {
		t.maxY = h - 1
	}
	// The digitizer's axes don't necessarily match the display's. If the
	// aspect orientations disagree, swap x/y unless the config forces it.
	if !cfg.SwapSet {
		t.cfg.Swap = (t.maxX > t.maxY) != (w > h)
		log.Printf("input: auto swap=%v (touch %dx%d vs screen %dx%d)", t.cfg.Swap, t.maxX, t.maxY, w, h)
	}
	// Grab so Nickel stops seeing touches while we run.
	grab := 1
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
	log.Printf("input: using %s, abs max %dx%d, grabbed", path, t.maxX, t.maxY)
	return t, nil
}

func (t *TouchReader) Ungrab() {
	grab := 0
	syscall.Syscall(syscall.SYS_IOCTL, t.f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
	t.f.Close()
}

// GrabDevice exclusively grabs an input device by name fragment and drains
// (discards) its events, so Nickel never sees them. Used for the
// accelerometer: rotation events would otherwise make Nickel repaint over us.
func GrabDevice(nameFragment string) (func(), error) {
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		name := make([]byte, 256)
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgname(256), uintptr(unsafe.Pointer(&name[0])))
		n := strings.ToLower(strings.TrimRight(string(name), "\x00"))
		if strings.Contains(n, nameFragment) {
			grab := 1
			syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
			log.Printf("input: grabbed %s (%q), draining", path, n)
			go func() {
				buf := make([]byte, 16*64)
				for {
					if _, err := f.Read(buf); err != nil {
						return
					}
				}
			}()
			return func() { f.Close() }, nil
		}
		f.Close()
	}
	return nil, fmt.Errorf("no device matching %q", nameFragment)
}

// KeyReader reads the physical page-turn buttons (gpio-keys device).
type KeyReader struct{ f *os.File }

func OpenKeys() (*KeyReader, error) {
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		name := make([]byte, 256)
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgname(256), uintptr(unsafe.Pointer(&name[0])))
		n := strings.ToLower(strings.TrimRight(string(name), "\x00"))
		if strings.Contains(n, "gpio-keys") || strings.Contains(n, "gpio_keys") {
			// With Nickel dead this grab succeeds (Nickel holds it
			// exclusively while alive — that cost us a day to learn).
			grab := 1
			_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
			log.Printf("keys: using %s (%q), grab errno=%d", path, n, errno)
			return &KeyReader{f: f}, nil
		}
		f.Close()
	}
	return nil, fmt.Errorf("no gpio-keys device found")
}

func (k *KeyReader) Ungrab() {
	grab := 0
	syscall.Syscall(syscall.SYS_IOCTL, k.f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
	k.f.Close()
}

// Run emits the evdev key code of each physical button press.
func (k *KeyReader) Run(out chan<- int) {
	buf := make([]byte, 16*16)
	logged := 0
	for {
		n, err := k.f.Read(buf)
		if err != nil {
			log.Printf("keys: read: %v", err)
			return
		}
		for off := 0; off+16 <= n; off += 16 {
			typ := binary.LittleEndian.Uint16(buf[off+8:])
			code := binary.LittleEndian.Uint16(buf[off+10:])
			val := int32(binary.LittleEndian.Uint32(buf[off+12:]))
			if typ != evSyn && logged < 100 {
				logged++
				log.Printf("keys: ev type=%d code=%d val=%d", typ, code, val)
			}
			if typ == evKey && val == 1 { // key press (not release/repeat)
				out <- int(code)
			}
		}
	}
}

// SpyDevice logs EV_KEY events from a device without grabbing it — used to
// hunt down which input device the physical page buttons live on.
func SpyDevice(nameFragment string, out chan<- int) {
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		name := make([]byte, 256)
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgname(256), uintptr(unsafe.Pointer(&name[0])))
		n := strings.ToLower(strings.TrimRight(string(name), "\x00"))
		if !strings.Contains(n, nameFragment) {
			f.Close()
			continue
		}
		log.Printf("spy: watching %s (%q)", path, n)
		go func(f *os.File, dev string) {
			buf := make([]byte, 16*16)
			logged := 0
			for {
				n, err := f.Read(buf)
				if err != nil {
					return
				}
				for off := 0; off+16 <= n; off += 16 {
					typ := binary.LittleEndian.Uint16(buf[off+8:])
					code := binary.LittleEndian.Uint16(buf[off+10:])
					val := int32(binary.LittleEndian.Uint32(buf[off+12:]))
					if typ == evKey && logged < 60 {
						logged++
						log.Printf("spy %s: key code=%d val=%d", dev, code, val)
						if val == 1 {
							out <- int(code)
						}
					}
				}
			}
		}(f, path)
		return
	}
}

func (t *TouchReader) mapPoint(rx, ry int) (int, int) {
	fx := float64(rx) / float64(t.maxX)
	fy := float64(ry) / float64(t.maxY)
	if t.cfg.Swap {
		fx, fy = fy, fx
	}
	if t.cfg.MirX {
		fx = 1 - fx
	}
	if t.cfg.MirY {
		fy = 1 - fy
	}
	return clamp(int(fx*float64(t.W)), 0, t.W-1), clamp(int(fy*float64(t.H)), 0, t.H-1)
}

// Run parses evdev packets into Touch events. Full MT protocol-B slot
// tracking: the first finger down is the "primary" contact and streams
// Down/Move/Up; additional fingers emit a tap (Down+Up) on release so fast
// two-thumb typing on the on-screen keyboard doesn't drop keys.
func (t *TouchReader) Run(out chan<- Touch) {
	buf := make([]byte, 16*64)
	type slot struct {
		track int
		x, y  int
	}
	var slots [10]slot
	for i := range slots {
		slots[i] = slot{track: -1, x: -1, y: -1}
	}
	active := [10]bool{}
	cur := 0
	primary := -1
	movesLogged := 0
	for {
		n, err := t.f.Read(buf)
		if err != nil {
			log.Printf("input: read: %v", err)
			close(out)
			return
		}
		for off := 0; off+16 <= n; off += 16 {
			typ := binary.LittleEndian.Uint16(buf[off+8:])
			code := binary.LittleEndian.Uint16(buf[off+10:])
			val := int32(binary.LittleEndian.Uint32(buf[off+12:]))
			switch typ {
			case evAbs:
				switch code {
				case 0x2f: // ABS_MT_SLOT
					if val >= 0 && int(val) < len(slots) {
						cur = int(val)
					}
				case absMTTrackingID:
					slots[cur].track = int(val)
				case absMTPositionX, absX:
					slots[cur].x = int(val)
				case absMTPositionY, absY:
					slots[cur].y = int(val)
				}
			case evKey:
				if code == btnTouch { // single-touch fallback devices
					if val > 0 {
						slots[0].track = 1
					} else {
						slots[0].track = -1
					}
				}
			case evSyn:
				if code != synReport {
					continue
				}
				for i := range slots {
					is := slots[i].track >= 0 && slots[i].x >= 0 && slots[i].y >= 0
					was := active[i]
					mx, my := t.mapPoint(slots[i].x, slots[i].y)
					switch {
					case is && !was:
						active[i] = true
						if primary == -1 {
							primary = i
							movesLogged = 0
							if t.logged < 40 {
								t.logged++
								log.Printf("input: down raw=(%d,%d) mapped=(%d,%d)", slots[i].x, slots[i].y, mx, my)
							}
							out <- Touch{Kind: TouchDown, X: mx, Y: my}
						}
					case is && was && i == primary:
						if movesLogged < 3 && t.logged < 40 {
							movesLogged++
							log.Printf("input: move raw=(%d,%d) mapped=(%d,%d)", slots[i].x, slots[i].y, mx, my)
						}
						out <- Touch{Kind: TouchMove, X: mx, Y: my}
					case !is && was:
						active[i] = false
						if i == primary {
							primary = -1
							out <- Touch{Kind: TouchUp, X: mx, Y: my}
						} else {
							// secondary finger: deliver as a tap of its own
							out <- Touch{Kind: TouchDown, X: mx, Y: my}
							out <- Touch{Kind: TouchUp, X: mx, Y: my}
						}
					}
				}
			}
		}
	}
}
