//go:build linux && !sim

package main

import (
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func readSysfsInt(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, false
	}
	return v, true
}

// BatteryStatus returns capacity percent (-1 if unknown) and charging state.
func BatteryStatus() (int, bool) {
	caps, _ := filepath.Glob("/sys/class/power_supply/*/capacity")
	for _, p := range caps {
		if v, ok := readSysfsInt(p); ok {
			charging := false
			if s, err := os.ReadFile(filepath.Join(filepath.Dir(p), "status")); err == nil {
				charging = strings.Contains(string(s), "Charging")
			}
			return v, charging
		}
	}
	return -1, false
}

// SetFrontlight writes a brightness percentage to the backlight device.
func SetFrontlight(pct int) {
	brs, _ := filepath.Glob("/sys/class/backlight/*/brightness")
	for _, p := range brs {
		if max, ok := readSysfsInt(filepath.Join(filepath.Dir(p), "max_brightness")); ok && max > 0 {
			v := pct * max / 100
			if err := os.WriteFile(p, []byte(strconv.Itoa(v)), 0644); err != nil {
				log.Printf("frontlight: write %s: %v", p, err)
			}
			return
		}
	}
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

// FrontlightPercent returns the frontlight level if a backlight device exists.
func FrontlightPercent() (int, bool) {
	brs, _ := filepath.Glob("/sys/class/backlight/*/actual_brightness")
	for _, p := range brs {
		if v, ok := readSysfsInt(p); ok {
			if max, ok := readSysfsInt(filepath.Join(filepath.Dir(p), "max_brightness")); ok && max > 0 {
				return v * 100 / max, true
			}
		}
	}
	return 0, false
}
