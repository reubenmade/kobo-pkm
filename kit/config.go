package kit

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config is the device/calibration/sleep configuration shared by every
// experiment, parsed from a `key = value` config.txt next to the binary.
// Experiment-specific keys land in Extra (accessed via Str/Int/Bool/Float),
// so one parser serves the whole repo.
type Config struct {
	// Display / input calibration.
	Rot      int  // -1 = auto (portrait); 0..3 forces logical->native rotation
	Swap     bool // swap touch x/y
	SwapSet  bool // swap explicitly set (else auto-detected from aspect)
	MirX     bool
	MirY     bool
	TouchDev string
	// PenTouchMin: minimum digitizer pressure (raw units, 0..4095) that counts
	// as pen contact. Below it the pen is hovering. Only used when a pressure
	// axis exists.
	PenTouchMin int
	// InputDebug logs one line per digitizer frame (first few thousand) — the
	// raw material for decoding hover / eraser / button behaviour.
	InputDebug bool

	// EinkWait blocks each refresh until the panel finishes painting it, pacing
	// rapid redraws so they don't queue in the controller. On by default; set
	// "eink_wait = 0" to fall back to fire-and-forget if a firmware misbehaves.
	EinkWait bool

	// Sleep.
	WakeGPIO bool // arm gpio-keys as a suspend wake source (cover wake) — EXPERIMENTAL
	// StateExtended is written to /sys/power/state-extended (gSleep_Mode_Suspend):
	// "0" (default) suspends like Nickel — page buttons, cover, power all wake;
	// "1" is KOReader's deeper suspend — only cover/power wake. "skip" leaves it.
	StateExtended string

	// Safety.
	DeadmanMin int // auto-exit after N minutes; 0 disables

	// Extra holds every unrecognised key for the experiment to interpret.
	Extra map[string]string
}

// DefaultConfig returns the baseline before config.txt is applied.
func DefaultConfig() Config {
	return Config{
		Rot:           -1,
		PenTouchMin:   30,
		StateExtended: "0",
		DeadmanMin:    60,
		EinkWait:      true,
		Extra:         map[string]string{},
	}
}

// LoadConfig reads config.txt (missing file is fine — defaults stand).
func LoadConfig(path string) Config {
	cfg := DefaultConfig()
	if f, err := os.Open(path); err == nil {
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
			cfg.Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	return cfg
}

func truthy(v string) bool {
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

// Set applies one key/value, routing unknown keys into Extra.
func (c *Config) Set(k, v string) {
	switch k {
	case "rot":
		if n, err := strconv.Atoi(v); err == nil {
			c.Rot = n
		}
	case "swap":
		if v == "auto" {
			return
		}
		c.Swap = truthy(v)
		c.SwapSet = true
	case "mirx":
		c.MirX = truthy(v)
	case "miry":
		c.MirY = truthy(v)
	case "touchdev":
		c.TouchDev = v
	case "pen_touch_min":
		if n, err := strconv.Atoi(v); err == nil {
			c.PenTouchMin = n
		}
	case "input_debug":
		c.InputDebug = truthy(v)
	case "eink_wait":
		c.EinkWait = truthy(v)
	case "wake_gpio":
		c.WakeGPIO = truthy(v)
	case "state_extended":
		c.StateExtended = v
	case "deadman_min":
		if n, err := strconv.Atoi(v); err == nil {
			c.DeadmanMin = n
		}
	default:
		if c.Extra == nil {
			c.Extra = map[string]string{}
		}
		c.Extra[k] = v
	}
}

// Str returns an Extra value or a default.
func (c Config) Str(key, def string) string {
	if v, ok := c.Extra[key]; ok {
		return v
	}
	return def
}

// Int returns an Extra value parsed as int, or a default.
func (c Config) Int(key string, def int) int {
	if v, ok := c.Extra[key]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Bool returns an Extra value parsed as bool, or a default.
func (c Config) Bool(key string, def bool) bool {
	if v, ok := c.Extra[key]; ok {
		return truthy(v)
	}
	return def
}

// Float returns an Extra value parsed as float64, or a default.
func (c Config) Float(key string, def float64) float64 {
	if v, ok := c.Extra[key]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
