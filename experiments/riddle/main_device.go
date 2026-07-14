//go:build linux && !sim

package main

import (
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// The diary on the Kobo. Launched by run.sh with Nickel dead (takeover):
// the framebuffer, touchscreen, and buttons are ours; run.sh restarts
// Nickel on any exit.

func main() {
	base := "/mnt/onboard/.adds/riddle"
	if len(os.Args) > 1 {
		base = os.Args[1]
	}
	os.MkdirAll(base, 0o755)
	var logFile *os.File
	if lf, err := os.OpenFile(filepath.Join(base, "log.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(lf)
		logFile = lf
		// FAT loses unsynced writes on a hard restart — fsync every 2s so
		// the log survives whatever killed us. Syncing the FILE is not
		// enough when it was freshly created: the DIRECTORY entry needs its
		// own fsync, or a power cut unlinks the whole log (two sessions
		// vanished that way).
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
	log.Printf("---- the diary opens ----")

	// A panic anywhere in main must reach the log (run.sh restores Nickel
	// on any exit, so dying loudly beats wedging silently).
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

	cfg := LoadConfig(filepath.Join(base, "config.txt"))
	InitFonts()
	pmProbe()
	logNet()
	// Arming gpio-keys as a wakeup source was meant to let the cover wake
	// the diary, but the first device it was tried on stopped waking AT ALL
	// (power button included) — opt-in until decoded. Power-button wake
	// works without it.
	if cfg.WakeGPIO {
		enableWakeup("gpio")
	}

	fb, err := OpenFB(cfg)
	if err != nil {
		log.Fatalf("fb: %v", err)
	}
	defer fb.Close()

	store := OpenMemory(cfg, base)
	if store != nil {
		log.Printf("memory holds %d pages", len(store.Entries))
	}
	oracle := SpawnOracle(cfg, store != nil)
	if oracle == nil {
		log.Printf("no oracle configured (set oracle_key in config.txt)")
	}

	diary := NewDiary(fb, cfg, oracle, store, "/tmp/riddle-page.png")

	// Paint BEFORE grabbing input: if the display path is broken the user
	// still controls the device.
	log.Printf("first paint done, grabbing input")

	touch, err := OpenTouch(cfg, fb.Bounds().Dx(), fb.Bounds().Dy())
	if err != nil {
		log.Fatalf("touch: %v", err)
	}
	defer touch.Ungrab()

	// Keep rotation events contained.
	if release, err := GrabDevice("accel"); err == nil {
		defer release()
	}

	// The power button sleeps the diary. Left ungrabbed so a long hard
	// press can always power the device off.
	keys := make(chan int, 8)
	if pk, err := OpenKeysByName("pwrkey", false); err == nil {
		defer pk.Close()
		go pk.Run(keys)
	} else {
		log.Printf("keys: %v (power button not watched)", err)
	}
	// Physical page-turn buttons are free with Nickel dead — future
	// interaction hooks; codes land in the log for now.
	if gk, err := OpenKeysByName("gpio", true); err == nil {
		defer gk.Close()
		go gk.Run(keys)
	}

	// exitClean shows a visible confirmation, releases input, and exits —
	// every exit path uses it so the user always knows the app is gone.
	exitClean := func(why string) {
		log.Printf("exit: %s", why)
		c := fb.Canvas()
		FillRect(c, fb.Bounds(), WHITE)
		blitCentered(c, "The diary closes.", 96.0, 0, fb.Bounds().Dx(), fb.Bounds().Dy()/2-120)
		blitCentered(c, "The Kobo home returns in a moment.", 48.0, 0, fb.Bounds().Dx(), fb.Bounds().Dy()/2+40)
		fb.Refresh(fb.Bounds(), RefreshFull)
		time.Sleep(400 * time.Millisecond) // let the update reach the panel
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

	// Dead-man switch: never wedge the device forever.
	if cfg.DeadmanMin > 0 {
		go func() {
			time.Sleep(time.Duration(cfg.DeadmanMin) * time.Minute)
			exitClean("dead-man timer")
		}()
	}

	// Exit when a USB connection appears: Nickel must own the Connect
	// dialog. Watch the battery's charging status, not the supply "online"
	// bit — that bit reads 1 permanently on this PMIC (which silently
	// disabled this exit for every session until it was noticed).
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

	// Stall watchdog: the main loop beats this counter every iteration. If
	// it stops for 10s (a wedge — corner-exit would be dead too), dump every
	// goroutine's stack to the log and exit so run.sh restores Nickel.
	// sleepDiary beats it too — suspend retries can hold the main loop for
	// tens of seconds legitimately (the EPD discharge timer blocks suspend
	// for up to ~30s after a screen update), and the watchdog "rescuing" a
	// sleeping diary would masquerade as a spontaneous reboot.
	var heartbeat atomic.Uint64
	sleepHeartbeat = &heartbeat
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

	// Corner-exit: 3 consecutive touch-downs in a top corner leave the
	// diary. The corner set is closed under any swap/mirror miscalibration.
	corner := 0
	W := fb.Bounds().Dx()
	powerGrace := time.Now()

	for {
		heartbeat.Add(1)
	drain:
		for {
			select {
			case t, ok := <-touches:
				if !ok {
					exitClean("touch device lost")
				}
				if t.Kind == TouchDown {
					if (t.X < 160 || t.X > W-160) && t.Y < 160 {
						corner++
						if corner >= 3 {
							exitClean("corner-exit")
						}
					} else {
						corner = 0
					}
				}
				diary.HandleTouch(t)
			case code := <-keys:
				log.Printf("key: %d", code)
				// 116 = power button; 35 = cover CLOSED (gpio-keys, with
				// autorepeat while it stays closed). 59 is left inert until
				// decoded — if it's the cover-open edge, sleeping on it
				// would re-suspend the diary the moment the cover lifts.
				if (code == 116 || code == 35) && time.Now().After(powerGrace) {
					sleepDiary(fb, diary, touches, keys)
					powerGrace = time.Now().Add(3 * time.Second)
				}
			default:
				break drain
			}
		}
		diary.Step()
		time.Sleep(3 * time.Millisecond)
	}
}

// sleepDiary: the page turns to "The diary sleeps.", the device suspends,
// and waking restores the page exactly.
//
// The suspend ritual is Kobo-specific (learned the hard way — a bare
// `echo mem` half-suspends and the device CRASH-REBOOTS on wake, which is
// also what kept stranding NickelMenu's failsafe):
//  1. frontlight off in software — its LED is independently powered and
//     stays lit through suspend otherwise
//  2. `1 > /sys/power/state-extended` — the NTX kernel's PM prepare hook —
//     then settle and sync (KOReader's sequence)
//  3. `mem > /sys/power/state`, confirmed via the kernel's success counter;
//     the EPD regulator refuses to sleep while its post-update discharge
//     timer (≤30s) runs, so retry until the counter moves
//  4. on wake: `0 > state-extended`, frontlight back, restore the page
//
// sleepHeartbeat lets sleepDiary keep beating the main loop's watchdog:
// legitimate suspend retries hold the main goroutine far past the stall
// threshold.
var sleepHeartbeat *atomic.Uint64

func sleepDiary(fb *FB, diary *Diary, touches chan Touch, keys chan int) {
	beat := func() {
		if sleepHeartbeat != nil {
			sleepHeartbeat.Add(1)
		}
	}
	wifiWasUp := wifiIsUp()
	log.Printf("sleeping (power button / cover); wifi up=%v charging=%v usbOnline=%v",
		wifiWasUp, isCharging(), usbOnline())

	// KOReader: "any suspend/standby attempt while plugged-in will hang the
	// kernel" on MTK. Guard on the battery's charging STATUS — a charger
	// node's "online" bit can read 1 permanently on some PMICs, which would
	// block sleep forever.
	if isCharging() {
		log.Printf("charging: NOT suspending (MTK kernel hazard); staying awake")
		return
	}

	saved := ShowSleep(fb.Canvas())
	fb.Refresh(fb.Bounds(), RefreshFull)
	time.Sleep(800 * time.Millisecond) // let the flash finish before power loss

	fl, hadFL := FrontlightPercent()
	if hadFL {
		SetFrontlight(0)
	}
	// KOReader kills Wi-Fi before every suspend — the MTK wmt driver blocks
	// (and on this FW, EPERMs) suspend while the radio is up.
	if wifiWasUp {
		wifiDown()
		beat()
	}
	if diary.cfg.StateExtended != "skip" {
		writeSysfs("/sys/power/state-extended", diary.cfg.StateExtended)
	}
	beat()
	time.Sleep(2 * time.Second) // KOReader settles exactly this long; PM hooks need it
	syscall.Sync()

	// This MTK kernel runs Android-style AUTOSLEEP: while /sys/power/autosleep
	// is armed, direct writes to /sys/power/state are refused with EPERM
	// (empirical; it's the kernel's state_store() behaviour). Disarm it for
	// the duration of our hand-driven suspend, restore on wake.
	autosleep0 := readPM("autosleep")
	if autosleep0 != "" && autosleep0 != "off" {
		log.Printf("pm: autosleep was %q — disarming for manual suspend", autosleep0)
		writeSysfs("/sys/power/autosleep", "off")
	}

	// A /sys/power/state write BLOCKS through the whole suspend and only
	// returns nil after resume — that return IS the wake signal. (This
	// kernel has no /sys/power/suspend_stats; polling a success counter
	// here spent a day mislabeling real sleeps as failures.) Errors mean
	// the kernel refused (EPERM right after wifi-down teardown, EBUSY from
	// the EPD discharge timer) — retry those.
	for attempt := 1; ; attempt++ {
		beat()
		// Wall-clock via Unix seconds: Go's monotonic clock PAUSES during
		// suspend, so time.Since would report a 99s sleep as "0s".
		t0 := time.Now().Unix()
		err := os.WriteFile("/sys/power/state", []byte("mem"), 0o644)
		if err == nil {
			log.Printf("suspend returned after %ds — we slept and woke", time.Now().Unix()-t0)
			break
		}
		log.Printf("suspend refused (%v) after %ds", err, time.Now().Unix()-t0)
		// Keep the input channels drained while we hold the main loop —
		// queued events must not block the readers or replay later.
		for {
			select {
			case <-touches:
				continue
			case <-keys:
				continue
			default:
			}
			break
		}
		if attempt >= 10 {
			log.Printf("suspend never accepted (%d tries); waking the page", attempt)
			break
		}
		time.Sleep(3 * time.Second)
		beat()
	}
	log.Printf("waking")
	// Screen FIRST: the writer judges wake by the page coming back, and the
	// wifi revival below costs ~5s — doing it before the repaint made every
	// wake look dead long enough to invite a second button press.
	if diary.cfg.StateExtended != "skip" {
		writeSysfs("/sys/power/state-extended", "0")
	}
	if hadFL {
		SetFrontlight(fl)
	}
	RestoreSleep(fb.Canvas(), saved)
	fb.Refresh(fb.Bounds(), RefreshFull)
	if autosleep0 != "" && autosleep0 != "off" {
		writeSysfs("/sys/power/autosleep", autosleep0)
	}
	if wifiWasUp {
		wifiUp()
	}
	// Discard input that queued while asleep — stale events would otherwise
	// replay as phantom ink on the restored page.
	for {
		select {
		case <-touches:
		case <-keys:
		default:
			return
		}
	}
}

func writeSysfs(path, val string) {
	if _, err := os.Stat(path); err != nil {
		log.Printf("pm: %s absent, skipping", path)
		return
	}
	if err := os.WriteFile(path, []byte(val), 0o644); err != nil {
		log.Printf("pm: write %s=%s: %v", path, val, err)
	}
}

// sh runs a small shell command, logging output/errors.
func sh(cmd string) {
	out, err := exec.Command("/bin/sh", "-c", cmd).CombinedOutput()
	if len(out) > 0 || err != nil {
		log.Printf("sh[%s]: %s %v", cmd, strings.TrimSpace(string(out)), err)
	}
}

func wifiIsUp() bool {
	b, err := os.ReadFile("/sys/class/net/wlan0/operstate")
	return err == nil && strings.TrimSpace(string(b)) == "up"
}

// wifiDown powers the radio off (KOReader does this before every suspend).
func wifiDown() {
	log.Printf("wifi: down for suspend")
	sh("ifconfig wlan0 down")
	sh("echo 0 > /dev/wmtWifi")
}

// wifiUp restores the radio after wake, mirroring run.sh's revival: chip on,
// interface up, wpa_supplicant (Nickel's saved-network config) if dead, and
// the dhcpcd master (or udhcpc) for the lease.
func wifiUp() {
	log.Printf("wifi: reviving after wake")
	sh("echo 1 > /dev/wmtWifi && sleep 2 && ifconfig wlan0 up")
	sh("pkill -0 wpa_supplicant || wpa_supplicant -D nl80211,wext -s -i wlan0 -c /etc/wpa_supplicant/wpa_supplicant.conf -C /var/run/wpa_supplicant -B")
	sh("pkill -0 dhcpcd || dhcpcd wlan0 || udhcpc -S -i wlan0 -t15 -T10 -A3 -b -q &")
	logNet()
}

// logNet records wlan0's state at startup: Nickel powers the Wi-Fi chip
// down as it dies, so this is where "no oracle" diagnoses start.
func logNet() {
	op, err := os.ReadFile("/sys/class/net/wlan0/operstate")
	if err != nil {
		log.Printf("net: wlan0 absent (%v)", err)
		return
	}
	carrier, _ := os.ReadFile("/sys/class/net/wlan0/carrier")
	log.Printf("net: wlan0 operstate=%s carrier=%s",
		strings.TrimSpace(string(op)), strings.TrimSpace(string(carrier)))
}

// readTrim reads any sysfs file, trimmed; "" if absent.
func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readPM reads a /sys/power file, trimmed; "" if absent.
func readPM(name string) string {
	return readTrim("/sys/power/" + name)
}

// pmProbe logs the power-management landscape once at startup, so suspend
// behaviour is never diagnosed blind again.
func pmProbe() {
	for _, f := range []string{"state", "mem_sleep", "autosleep", "wake_lock", "state-extended"} {
		if v := readPM(f); v != "" {
			log.Printf("pm: /sys/power/%s = %q", f, v)
		} else {
			log.Printf("pm: /sys/power/%s absent/empty", f)
		}
	}
	names, _ := filepath.Glob("/sys/class/input/input*/name")
	for _, p := range names {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		wk, _ := os.ReadFile(filepath.Join(filepath.Dir(p), "device", "power", "wakeup"))
		log.Printf("pm: input %q wakeup=%q", strings.TrimSpace(string(b)), strings.TrimSpace(string(wk)))
	}
	// The cover's hall sensor may live on a dedicated platform device rather
	// than an input one — list every platform wakeup control for candidates.
	plats, _ := filepath.Glob("/sys/devices/platform/*/power/wakeup")
	for _, p := range plats {
		v, _ := os.ReadFile(p)
		log.Printf("pm: platform %s wakeup=%q", filepath.Base(filepath.Dir(filepath.Dir(p))), strings.TrimSpace(string(v)))
	}
	// Power supplies: is "online" trustworthy, is "status" sane?
	sups, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, p := range sups {
		on, _ := os.ReadFile(filepath.Join(p, "online"))
		st, _ := os.ReadFile(filepath.Join(p, "status"))
		log.Printf("pm: supply %s online=%q status=%q", filepath.Base(p),
			strings.TrimSpace(string(on)), strings.TrimSpace(string(st)))
	}
	// NTX kernels sometimes hang custom wake knobs off the key driver —
	// list gpio-keys' attributes so we know what's on offer.
	if ents, err := os.ReadDir("/sys/devices/platform/gpio-keys"); err == nil {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		log.Printf("pm: gpio-keys attrs: %s", strings.Join(names, " "))
	}
}

// isCharging reads the battery's charging status — the reliable "plugged
// in" signal (KOReader guards MTK suspend on exactly this). Exact match:
// "Discharging" CONTAINS "Charging", and "Full" only happens on power.
func isCharging() bool {
	sts, _ := filepath.Glob("/sys/class/power_supply/*/status")
	for _, p := range sts {
		if b, err := os.ReadFile(p); err == nil {
			s := strings.TrimSpace(string(b))
			if s == "Charging" || s == "Full" {
				return true
			}
		}
	}
	return false
}

// enableWakeup arms an input device (by name fragment) as a system wakeup
// source. The cover's hall sensor ships disabled — Nickel arms it at sleep
// time — so without this only the power button can wake a suspended diary.
func enableWakeup(nameFragment string) {
	names, _ := filepath.Glob("/sys/class/input/input*/name")
	for _, p := range names {
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(strings.ToLower(string(b)), nameFragment) {
			continue
		}
		name := strings.TrimSpace(string(b))
		wk := filepath.Join(filepath.Dir(p), "device", "power", "wakeup")
		prev, err := os.ReadFile(wk)
		if err != nil {
			log.Printf("pm: %q has no wakeup control", name)
			continue
		}
		if err := os.WriteFile(wk, []byte("enabled"), 0o644); err != nil {
			log.Printf("pm: enabling wakeup for %q: %v", name, err)
		} else {
			log.Printf("pm: wakeup enabled for %q (was %s)", name, strings.TrimSpace(string(prev)))
		}
	}
}
