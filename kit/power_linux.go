//go:build linux && !sim

package kit

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The MTK (Libra Colour) suspend ritual. A bare `echo mem > /sys/power/state`
// half-suspends and the device crash-reboots on wake; the sequence below is
// what actually works, learned the hard way (see FIELD-NOTES.md). The App
// runner calls suspendRitual; screen save/restore is handled around it.
//
// beat() keeps the stall watchdog fed (legitimate suspend retries can hold the
// main goroutine for tens of seconds). drain() flushes queued input during
// retries so nothing replays as phantom taps on wake.
func suspendRitual(cfg Config, beat, drain func()) {
	wifiWasUp := wifiIsUp()
	log.Printf("suspend: wifi up=%v charging=%v usbOnline=%v", wifiWasUp, isCharging(), usbOnline())

	// KOReader: any suspend attempt while plugged in HANGS the MTK kernel.
	// Guard on the battery's charging STATUS — a charger node's "online" bit
	// reads 1 permanently on some PMICs and would block sleep forever.
	if isCharging() {
		log.Printf("suspend: charging — NOT suspending (MTK kernel hazard)")
		return
	}

	fl, hadFL := FrontlightPercent()
	if hadFL {
		SetFrontlight(0) // its LED is independently powered; stays lit otherwise
	}
	if wifiWasUp {
		wifiDown() // the wmt driver blocks/EPERMs suspend while the radio is up
		beat()
	}
	if cfg.StateExtended != "skip" {
		writeSysfs("/sys/power/state-extended", cfg.StateExtended)
	}
	beat()
	time.Sleep(2 * time.Second) // KOReader settles exactly this long; PM hooks need it
	syscall.Sync()

	// This kernel runs Android-style AUTOSLEEP: while /sys/power/autosleep is
	// armed, direct writes to /sys/power/state EPERM. Disarm for the manual
	// suspend, restore on wake.
	autosleep0 := readPM("autosleep")
	if autosleep0 != "" && autosleep0 != "off" {
		log.Printf("suspend: autosleep was %q — disarming", autosleep0)
		writeSysfs("/sys/power/autosleep", "off")
	}

	// The state write BLOCKS through the whole sleep and returns nil only after
	// resume — that return IS the wake signal. This kernel has no
	// /sys/power/suspend_stats, so polling a success counter mislabels real
	// sleeps as failures. Errors mean the kernel refused (EPERM after wifi
	// teardown, EBUSY from the EPD discharge timer) — retry those.
	for attempt := 1; ; attempt++ {
		beat()
		// Wall-clock via Unix seconds: Go's monotonic clock PAUSES during
		// suspend, so time.Since would report a 99s sleep as "0s".
		t0 := time.Now().Unix()
		err := os.WriteFile("/sys/power/state", []byte("mem"), 0o644)
		if err == nil {
			log.Printf("suspend: returned after %ds — slept and woke", time.Now().Unix()-t0)
			break
		}
		log.Printf("suspend: refused (%v) after %ds", err, time.Now().Unix()-t0)
		drain()
		if attempt >= 10 {
			log.Printf("suspend: never accepted (%d tries); waking", attempt)
			break
		}
		time.Sleep(3 * time.Second)
		beat()
	}

	log.Printf("suspend: waking")
	if cfg.StateExtended != "skip" {
		writeSysfs("/sys/power/state-extended", "0")
	}
	if hadFL {
		SetFrontlight(fl)
	}
	if autosleep0 != "" && autosleep0 != "off" {
		writeSysfs("/sys/power/autosleep", autosleep0)
	}
	if wifiWasUp {
		wifiUp()
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

func wifiDown() {
	log.Printf("wifi: down for suspend")
	sh("ifconfig wlan0 down")
	sh("echo 0 > /dev/wmtWifi")
}

func wifiUp() {
	log.Printf("wifi: reviving after wake")
	sh("echo 1 > /dev/wmtWifi && sleep 2 && ifconfig wlan0 up")
	sh("pkill -0 wpa_supplicant || wpa_supplicant -D nl80211,wext -s -i wlan0 -c /etc/wpa_supplicant/wpa_supplicant.conf -C /var/run/wpa_supplicant -B")
	sh("pkill -0 dhcpcd || dhcpcd wlan0 || udhcpc -S -i wlan0 -t15 -T10 -A3 -b -q &")
	logNet()
}

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

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readPM(name string) string { return readTrim("/sys/power/" + name) }

// pmProbe logs the power-management landscape once at startup, so suspend
// behaviour is never diagnosed blind.
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
	sups, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, p := range sups {
		on, _ := os.ReadFile(filepath.Join(p, "online"))
		st, _ := os.ReadFile(filepath.Join(p, "status"))
		log.Printf("pm: supply %s online=%q status=%q", filepath.Base(p),
			strings.TrimSpace(string(on)), strings.TrimSpace(string(st)))
	}
}

// isCharging reads the battery's charging status — the reliable "plugged in"
// signal (KOReader guards MTK suspend on exactly this). Exact match:
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
// source. The cover's hall sensor ships disabled; without this only the power
// button wakes a suspended app. EXPERIMENTAL — broke waking entirely once.
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
