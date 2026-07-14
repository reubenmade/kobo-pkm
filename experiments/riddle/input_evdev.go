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

// Touch/pen input straight from evdev, carried over from kobo-pkm/app. On
// the Kobo the stylus (MPP) arrives through the same elan touchscreen as
// fingers. The device is grabbed (EVIOCGRAB) so nothing else reacts.
//
// Diary-specific changes vs the kobo-pkm reader:
//   - pressure is parsed (ABS_MT_PRESSURE / ABS_PRESSURE) and normalized to
//     0..4096 so stroke width can follow it when the digitizer reports it
//   - only the primary contact emits events — a resting palm must not ink
//     the page or dismiss a panel
//   - BTN_TOOL_RUBBER is honored as the eraser if the hardware ever sends
//     it, and unknown EV_KEY codes are logged for discovery

const (
	evSyn = 0x00
	evKey = 0x01
	evAbs = 0x03

	synReport       = 0
	btnTouch        = 0x14a
	btnToolPen      = 0x140
	btnToolRubber   = 0x141
	btnStylus       = 0x14b // pen side (barrel) button
	btnStylus2      = 0x14c
	absX            = 0x00
	absY            = 0x01
	absPressure     = 0x18
	absDistance     = 0x19
	absMTSlot       = 0x2f
	absMTPositionX  = 0x35
	absMTPositionY  = 0x36
	absMTToolType   = 0x37
	absMTTrackingID = 0x39
	absMTPressure   = 0x3a
	absMTDistance   = 0x3b
	eviocgrab       = 0x40044590
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
	f          *os.File
	maxX, maxY int
	maxP       int
	cfg        Config
	W, H       int
	logged     int
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
	t := &TouchReader{f: f, cfg: cfg, W: w, H: h}
	var ai absInfo
	readAbs := func(code uint16) int {
		if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgabs(code), uintptr(unsafe.Pointer(&ai))); e == 0 && ai.Max > 0 {
			return int(ai.Max)
		}
		return 0
	}
	t.maxX = readAbs(absMTPositionX)
	t.maxY = readAbs(absMTPositionY)
	if t.maxX == 0 {
		t.maxX = readAbs(absX)
	}
	if t.maxY == 0 {
		t.maxY = readAbs(absY)
	}
	if t.maxX == 0 {
		t.maxX = w - 1
	}
	if t.maxY == 0 {
		t.maxY = h - 1
	}
	t.maxP = readAbs(absMTPressure)
	if t.maxP == 0 {
		t.maxP = readAbs(absPressure)
	}
	// The digitizer's axes don't necessarily match the display's. If the
	// aspect orientations disagree, swap x/y unless the config forces it.
	if !cfg.SwapSet {
		t.cfg.Swap = (t.maxX > t.maxY) != (w > h)
		log.Printf("input: auto swap=%v (touch %dx%d vs screen %dx%d)", t.cfg.Swap, t.maxX, t.maxY, w, h)
	}
	grab := 1
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
	log.Printf("input: using %s, abs max %dx%d maxP=%d, grabbed", path, t.maxX, t.maxY, t.maxP)
	return t, nil
}

func (t *TouchReader) Ungrab() {
	grab := 0
	syscall.Syscall(syscall.SYS_IOCTL, t.f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&grab)))
	t.f.Close()
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

// logEach logs the first few occurrences of each distinct code, so one
// chatty code (the hover pulse is key 236) can't drown out a rare one
// (the eraser, the side button). Keyed by code only: continuously-varying
// axes (tilt…) must not log once per distinct value.
type logEach map[uint16]int

func (l logEach) note(kind string, code uint16, val int32) {
	l[code]++
	if l[code] <= 3 {
		log.Printf("input: %s code=%d val=%d (seen %dx)", kind, code, val, l[code])
	}
}

// Run parses evdev packets into Touch events. Full MT protocol-B slot
// tracking; only the primary contact streams Down/Move/Up.
//
// Contact is PRESSURE-GATED when the device reports a pressure axis: the
// elan keeps the tracking ID alive while the pen merely hovers, so tracking
// alone would ink connecting lines between strokes. Hover frames go out as
// TouchHover (position only).
func (t *TouchReader) Run(out chan<- Touch) {
	buf := make([]byte, 16*64)
	type slot struct {
		track    int
		x, y     int
		pressure int
		dist     int
		tool     int
	}
	var slots [10]slot
	for i := range slots {
		slots[i] = slot{track: -1, x: -1, y: -1, tool: -1}
	}
	active := [10]bool{}
	cur := 0
	primary := -1
	eraser := false
	button := false
	btnTouchOn := false
	penDetect := false // key 236: pulses when the pen approaches (undecoded)
	keyLog := logEach{}
	absLog := logEach{}
	frames := 0
	lastDbg := "" // last logged state tuple, to log transitions immediately

	// A slot is in contact when tracked AND pressing (or when the device has
	// no pressure axis at all — then tracking is the best we have).
	// Pressure ALONE decides: the elan holds BTN_TOUCH for the whole
	// pen-in-range period, so it must not vouch for contact — hover frames
	// report p=0 (and distance 15, where contact reports 0). Fingers report
	// real pressure (~2000 raw), so they pass the same gate.
	touching := func(s slot) bool {
		if s.track < 0 || s.x < 0 || s.y < 0 {
			return false
		}
		if t.maxP > 0 {
			return s.pressure >= t.cfg.PenTouchMin
		}
		return true
	}

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
				case absMTSlot:
					if val >= 0 && int(val) < len(slots) {
						cur = int(val)
					}
				case absMTTrackingID:
					slots[cur].track = int(val)
					if val < 0 {
						// Contact gone: drop stale pressure, or the NEXT
						// hover would read the old value and ink instantly.
						slots[cur].pressure = 0
						slots[cur].dist = 0
					}
				case absMTPositionX, absX:
					slots[cur].x = int(val)
				case absMTPositionY, absY:
					slots[cur].y = int(val)
				case absMTPressure, absPressure:
					slots[cur].pressure = int(val)
				case absMTDistance, absDistance:
					slots[cur].dist = int(val)
				case absMTToolType:
					if slots[cur].tool != int(val) {
						log.Printf("input: slot %d tool type -> %d", cur, val)
					}
					slots[cur].tool = int(val)
				default:
					absLog.note("abs", code, val)
				}
			case evKey:
				switch code {
				case btnTouch:
					btnTouchOn = val > 0
					if t.maxP == 0 { // single-touch fallback devices
						if val > 0 {
							slots[0].track = 1
						} else {
							slots[0].track = -1
						}
					}
				case btnToolRubber, btnStylus: // 331: the tail eraser
					eraser = val == 1
					log.Printf("input: eraser (code %d) = %d", code, val)
				case btnStylus2: // 332: the pen's side button
					button = val == 1
					log.Printf("input: side button (332) = %d", val)
				case btnToolPen:
					keyLog.note("BTN_TOOL_PEN", code, val)
				case 236: // pen-detect pulse on the Libra Colour elan
					penDetect = val == 1
					keyLog.note("pen-detect", code, val)
				default:
					keyLog.note("key", code, val)
				}
			case evSyn:
				if code != synReport {
					continue
				}
				frames++
				for i := range slots {
					tracked := slots[i].track >= 0 && slots[i].x >= 0 && slots[i].y >= 0
					is := touching(slots[i])
					was := active[i]
					mx, my := t.mapPoint(slots[i].x, slots[i].y)
					p := slots[i].pressure
					if t.maxP > 0 {
						p = p * 4096 / t.maxP
					}
					if t.cfg.InputDebug && i == cur && tracked && frames <= 8000 {
						// Log every state transition immediately, but steady
						// motion only 1-in-8 — the log lives on slow FAT.
						state := fmt.Sprintf("tool=%d touch=%v er=%v btn=%v btnTouch=%v pd=%v",
							slots[i].tool, is, eraser, button, btnTouchOn, penDetect)
						if state != lastDbg || frames%8 == 0 {
							lastDbg = state
							log.Printf("dbg f=%d s%d track=%d xy=(%d,%d) p=%d d=%d %s",
								frames, i, slots[i].track, slots[i].x, slots[i].y,
								slots[i].pressure, slots[i].dist, state)
						}
					}
					switch {
					case is && !was:
						active[i] = true
						if primary == -1 {
							primary = i
							if t.logged < 40 {
								t.logged++
								log.Printf("input: down raw=(%d,%d) mapped=(%d,%d) p=%d", slots[i].x, slots[i].y, mx, my, p)
							}
							out <- Touch{Kind: TouchDown, X: mx, Y: my, Pressure: p, Eraser: eraser, Button: button}
						}
					case is && was && i == primary:
						out <- Touch{Kind: TouchMove, X: mx, Y: my, Pressure: p, Eraser: eraser, Button: button}
					case !is && was:
						active[i] = false
						if i == primary {
							primary = -1
							out <- Touch{Kind: TouchUp, X: mx, Y: my, Pressure: p, Eraser: eraser, Button: button}
						}
						// Secondary contacts (a resting palm) are dropped.
					case tracked && !is && primary == -1:
						// The pen gliding above the page.
						out <- Touch{Kind: TouchHover, X: mx, Y: my, Eraser: eraser, Button: button}
					}
				}
			}
		}
	}
}

// GrabDevice exclusively grabs an input device by name fragment and drains
// (discards) its events. Used for the accelerometer: rotation events would
// otherwise reach whoever else is listening.
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

// KeyReader reads a physical-button evdev device (gpio-keys, pwrkey…).
type KeyReader struct {
	f    *os.File
	name string
}

// OpenKeysByName finds a key device by name fragment. grab takes it
// exclusively (only possible with Nickel dead); the power key device is left
// ungrabbed so hard power-off always works.
func OpenKeysByName(fragment string, grab bool) (*KeyReader, error) {
	matches, _ := filepath.Glob("/dev/input/event*")
	for _, path := range matches {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		name := make([]byte, 256)
		syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgname(256), uintptr(unsafe.Pointer(&name[0])))
		n := strings.ToLower(strings.TrimRight(string(name), "\x00"))
		if strings.Contains(n, fragment) {
			if grab {
				g := 1
				_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), eviocgrab, uintptr(unsafe.Pointer(&g)))
				log.Printf("keys: using %s (%q), grab errno=%d", path, n, errno)
			} else {
				log.Printf("keys: using %s (%q), ungrabbed", path, n)
			}
			return &KeyReader{f: f, name: n}, nil
		}
		f.Close()
	}
	return nil, fmt.Errorf("no device matching %q", fragment)
}

func (k *KeyReader) Close() {
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
				log.Printf("keys[%s]: ev type=%d code=%d val=%d", k.name, typ, code, val)
			}
			if typ == evKey && val == 1 { // key press (not release/repeat)
				out <- int(code)
			}
		}
	}
}

func readSysfsInt(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var v int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &v); err != nil {
		return 0, false
	}
	return v, true
}

// usbOnline reports whether any USB/AC supply is online.
func usbOnline() bool {
	paths, _ := filepath.Glob("/sys/class/power_supply/*/online")
	for _, p := range paths {
		if v, ok := readSysfsInt(p); ok && v == 1 {
			return true
		}
	}
	return false
}

// FrontlightPercent returns the frontlight level if a backlight device
// exists. (From kobo-pkm; needed so sleep can douse the independently
// powered frontlight LED — suspend does not turn it off.)
func FrontlightPercent() (int, bool) {
	brs, _ := filepath.Glob("/sys/class/backlight/*/actual_brightness")
	for _, p := range brs {
		if v, ok := readSysfsInt(p); ok {
			if mx, ok := readSysfsInt(filepath.Join(filepath.Dir(p), "max_brightness")); ok && mx > 0 {
				return v * 100 / mx, true
			}
		}
	}
	return 0, false
}

// SetFrontlight writes a brightness percentage to the backlight device.
func SetFrontlight(pct int) {
	brs, _ := filepath.Glob("/sys/class/backlight/*/brightness")
	for _, p := range brs {
		if mx, ok := readSysfsInt(filepath.Join(filepath.Dir(p), "max_brightness")); ok && mx > 0 {
			v := pct * mx / 100
			if err := os.WriteFile(p, []byte(fmt.Sprintf("%d", v)), 0o644); err != nil {
				log.Printf("frontlight: write %s: %v", p, err)
			}
			return
		}
	}
}
