package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config tunes the device backend without recompiling. Loaded from
// config.txt next to the binary. Touch axis flags exist because Kobo
// panels differ in how the digitizer is mounted relative to the display —
// if taps land in the wrong place, flip these.
type Config struct {
	Rot      int // -1 = auto (portrait), 0..3 forces logical->native rotation
	Swap     bool
	SwapSet  bool // swap explicitly configured (otherwise auto-detected)
	MirX     bool
	MirY     bool
	TouchDev string
}

func LoadConfig(path string) Config {
	cfg := Config{Rot: -1}
	f, err := os.Open(path)
	if err != nil {
		return cfg
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "rot":
			if n, err := strconv.Atoi(v); err == nil {
				cfg.Rot = n
			}
		case "swap":
			if v == "auto" {
				break
			}
			cfg.Swap = v == "1" || v == "true"
			cfg.SwapSet = true
		case "mirx":
			cfg.MirX = v == "1" || v == "true"
		case "miry":
			cfg.MirY = v == "1" || v == "true"
		case "touchdev":
			cfg.TouchDev = v
		}
	}
	return cfg
}
