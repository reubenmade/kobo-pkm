//go:build linux && !sim

package kit

import (
	"image"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
)

// Physical key codes on the Libra Colour (see FIELD-NOTES.md).
const (
	KeyPageBack    = 193 // gpio-keys F23
	KeyPageForward = 194 // gpio-keys F24
	KeyPower       = 116 // pwrkey
	KeyCoverClosed = 35  // gpio-keys hall sensor, autorepeats while closed
)

// Handler is what an experiment implements. Every method runs on the single
// main goroutine, so it may freely draw into rt.Canvas() and call rt.Refresh
// without locking.
type Handler interface {
	Start()                    // first paint (before input is grabbed)
	Touch(t Touch)             // pen/finger; t.Button = pen side button held
	Key(code int)              // physical buttons (KeyPageBack/Forward, …)
	Step()                     // per-tick, for animation; keep it cheap
	SleepScreen(c *image.RGBA) // draw the screen shown while suspended
	ExitScreen(c *image.RGBA)  // draw the "app closed" splash
}

// Runtime is the handle an experiment gets: the framebuffer, config, and the
// controls it needs (refresh, request-sleep).
type Runtime struct {
	FB   *FB
	Cfg  Config
	Base string

	sleepReq atomic.Bool
}

func (rt *Runtime) Canvas() *image.RGBA               { return rt.FB.Canvas() }
func (rt *Runtime) Bounds() image.Rectangle           { return rt.FB.Bounds() }
func (rt *Runtime) Refresh(r image.Rectangle, m RefreshMode) { rt.FB.Refresh(r, m) }
func (rt *Runtime) RefreshAll(m RefreshMode)          { rt.FB.Refresh(rt.FB.Bounds(), m) }

// RequestSleep asks the runtime to suspend at the next loop turn (same path as
// the power button).
func (rt *Runtime) RequestSleep() { rt.sleepReq.Store(true) }

// Run is the whole Nickel-takeover lifecycle: logging, framebuffer, grabbed
// input, watchdog, corner-exit, USB-exit, power-button suspend, and a clean
// exit splash. mk builds the experiment's Handler from the Runtime. Launched by
// run.sh with Nickel dead; run.sh restarts Nickel on any exit.
func Run(cfg Config, base string, mk func(rt *Runtime) Handler) {
	os.MkdirAll(base, 0o755)
	var logFile *os.File
	if lf, err := os.OpenFile(filepath.Join(base, "log.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(lf)
		logFile = lf
		// FAT loses unsynced writes on a hard restart. Sync the file AND its
		// directory (a freshly-created file's dir entry needs its own fsync or
		// a power cut unlinks the whole log), then fsync every 2s.
		if dir, err := os.Open(base); err == nil {
			lf.Sync()
			dir.Sync()
			dir.Close()
		}
		go func() {
			for range time.Tick(2 * time.Second) {
				lf.Sync()
			}
		}()
	}
	log.Printf("---- kit app start (%s) ----", base)

	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 64*1024)
			log.Printf("PANIC: %v\n%s", r, buf[:runtime.Stack(buf, true)])
			if logFile != nil {
				logFile.Sync()
			}
			os.Exit(1)
		}
	}()

	InitFonts()
	pmProbe()
	logNet()
	if cfg.WakeGPIO {
		enableWakeup("gpio")
	}

	fb, err := OpenFB(cfg)
	if err != nil {
		log.Fatalf("fb: %v", err)
	}
	defer fb.Close()

	rt := &Runtime{FB: fb, Cfg: cfg, Base: base}
	h := mk(rt)

	// Paint BEFORE grabbing input: if the display path is broken the user still
	// controls the device.
	h.Start()
	log.Printf("first paint done, grabbing input")

	touch, err := OpenTouch(cfg, fb.Bounds().Dx(), fb.Bounds().Dy())
	if err != nil {
		log.Fatalf("touch: %v", err)
	}
	defer touch.Ungrab()

	if release, err := GrabDevice("accel"); err == nil {
		defer release()
	}

	keys := make(chan int, 8)
	if pk, err := OpenKeysByName("pwrkey", false); err == nil {
		defer pk.Close()
		go pk.Run(keys)
	} else {
		log.Printf("keys: %v (power button not watched)", err)
	}
	if gk, err := OpenKeysByName("gpio", true); err == nil {
		defer gk.Close()
		go gk.Run(keys)
	}

	exitClean := func(why string) {
		log.Printf("exit: %s", why)
		h.ExitScreen(fb.Canvas())
		fb.Refresh(fb.Bounds(), RefreshFull)
		time.Sleep(400 * time.Millisecond)
		touch.Ungrab()
		fb.Close()
		os.Exit(0)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		exitClean("signal")
	}()

	if cfg.DeadmanMin > 0 {
		go func() {
			time.Sleep(time.Duration(cfg.DeadmanMin) * time.Minute)
			exitClean("dead-man timer")
		}()
	}

	// Exit when USB connects: Nickel must own the Connect dialog. Watch the
	// battery's charging STATUS, not the supply "online" bit (which reads 1
	// permanently on this PMIC).
	go func() {
		prev := isCharging()
		for range time.Tick(3 * time.Second) {
			cur := isCharging()
			if cur && !prev {
				exitClean("usb connected")
			}
			prev = cur
		}
	}()

	touches := make(chan Touch, 256)
	go touch.Run(touches)

	// Stall watchdog: the main loop beats this every iteration. If it stops for
	// ~10s (a wedge — corner-exit would be dead too), dump every goroutine's
	// stack and exit so run.sh restores Nickel. Suspend beats it too, or a long
	// legitimate suspend would masquerade as a spontaneous reboot.
	var heartbeat atomic.Uint64
	beat := func() { heartbeat.Add(1) }
	go func() {
		last, stuck := uint64(0), 0
		for range time.Tick(2 * time.Second) {
			cur := heartbeat.Load()
			if cur == last {
				stuck++
				if stuck >= 5 {
					buf := make([]byte, 256*1024)
					log.Printf("STALL: main loop frozen ~10s; goroutines:\n%s", buf[:runtime.Stack(buf, true)])
					if logFile != nil {
						logFile.Sync()
					}
					exitClean("stall watchdog")
				}
			} else {
				stuck = 0
			}
			last = cur
		}
	}()

	drainInput := func() {
		for {
			select {
			case <-touches:
			case <-keys:
			default:
				return
			}
		}
	}

	doSleep := func() {
		// Save the page, show the sleep screen, suspend, restore exactly.
		b := fb.Bounds()
		saved := CopyRect(fb.Canvas(), b)
		h.SleepScreen(fb.Canvas())
		fb.Refresh(b, RefreshFull)
		time.Sleep(800 * time.Millisecond) // let the flash finish before power loss
		suspendRitual(cfg, beat, drainInput)
		// Screen FIRST on wake: the wifi revival costs ~5s and doing it before
		// the repaint makes every wake look dead.
		PasteRect(fb.Canvas(), b, saved)
		fb.Refresh(b, RefreshFull)
		drainInput()
	}

	corner := 0
	W := fb.Bounds().Dx()
	powerGrace := time.Now()

	for {
		beat()
		if rt.sleepReq.Swap(false) {
			doSleep()
			powerGrace = time.Now().Add(3 * time.Second)
		}
	drain:
		for {
			select {
			case t, ok := <-touches:
				if !ok {
					exitClean("touch device lost")
				}
				if t.Kind == TouchDown {
					// 3 taps in a top corner exit — the corner set is closed
					// under any swap/mirror miscalibration.
					if (t.X < 160 || t.X > W-160) && t.Y < 160 {
						corner++
						if corner >= 3 {
							exitClean("corner-exit")
						}
					} else {
						corner = 0
					}
				}
				h.Touch(t)
			case code := <-keys:
				log.Printf("key: %d", code)
				if (code == KeyPower || code == KeyCoverClosed) && time.Now().After(powerGrace) {
					doSleep()
					powerGrace = time.Now().Add(3 * time.Second)
				} else {
					h.Key(code)
				}
			default:
				break drain
			}
		}
		h.Step()
		time.Sleep(3 * time.Millisecond)
	}
}
