package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config comes from config.txt next to the binary (see config.txt.example).
// Touch axis flags exist because Kobo panels differ in how the digitizer is
// mounted relative to the display; the oracle settings mirror riddle's
// RIDDLE_OPENAI_* variables, which also work as environment overrides.
type Config struct {
	// Display / input calibration.
	Rot      int // -1 = auto (portrait), 0..3 forces logical->native rotation
	Swap     bool
	SwapSet  bool // swap explicitly configured (otherwise auto-detected)
	MirX     bool
	MirY     bool
	TouchDev string
	// Minimum digitizer pressure (raw units) that counts as pen contact —
	// below it the pen is hovering. Only used when a pressure axis exists.
	PenTouchMin int
	// InputDebug logs one line per digitizer frame (first ~4000 frames):
	// the raw material for decoding hover / eraser / button behaviour.
	InputDebug bool
	// WakeGPIO arms gpio-keys as a suspend wakeup source (cover wake).
	// EXPERIMENTAL: broke waking entirely on first trial — default off.
	WakeGPIO bool
	// StateExtended is what sleep writes to /sys/power/state-extended
	// (gSleep_Mode_Suspend): "1" = deep NTX suspend (default; cover+power
	// wake), "0"/"skip" = experiment — Nickel's sleep keeps the page
	// buttons wake-capable, ours doesn't, and this flag is the suspect.
	StateExtended string

	// The oracle. Key "fake" streams a canned reply (no network) — useful
	// on-device before Wi-Fi is sorted, and in the simulator.
	OracleKey       string
	OracleBase      string
	OracleModel     string
	OracleReasoning string
	OracleMaxTokens int

	// The diary's memory.
	MemoryOff   bool
	MemoryDir   string
	MemoryTurns int

	TZOffsetHours float64
	DeadmanMin    int // exit after this many minutes; 0 disables
	KeepPage      bool
}

func LoadConfig(path string) Config {
	cfg := Config{
		Rot:             -1,
		PenTouchMin:     30,
		StateExtended:   "1",
		OracleBase:      "https://api.openai.com/v1",
		OracleModel:     "gpt-4o-mini",
		OracleMaxTokens: 2000,
		MemoryTurns:     6,
		DeadmanMin:      60,
	}
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
			cfg.set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	// Environment overrides, same names as riddle's oracle.env.
	for env, key := range map[string]string{
		"RIDDLE_OPENAI_KEY":        "oracle_key",
		"RIDDLE_OPENAI_BASE":       "oracle_base",
		"RIDDLE_OPENAI_MODEL":      "oracle_model",
		"RIDDLE_OPENAI_REASONING":  "oracle_reasoning",
		"RIDDLE_OPENAI_MAX_TOKENS": "oracle_max_tokens",
		"RIDDLE_MEMORY":            "memory",
		"RIDDLE_MEMORY_DIR":        "memory_dir",
		"RIDDLE_MEMORY_TURNS":      "memory_turns",
		"RIDDLE_TZ_OFFSET":         "tz_offset",
		"RIDDLE_KEEP_PAGE":         "keep_page",
	} {
		if v, ok := os.LookupEnv(env); ok {
			cfg.set(key, v)
		}
	}
	return cfg
}

func (c *Config) set(k, v string) {
	truthy := v == "1" || v == "true" || v == "on" || v == "yes"
	switch k {
	case "rot":
		if n, err := strconv.Atoi(v); err == nil {
			c.Rot = n
		}
	case "swap":
		if v == "auto" {
			break
		}
		c.Swap = truthy
		c.SwapSet = true
	case "mirx":
		c.MirX = truthy
	case "miry":
		c.MirY = truthy
	case "touchdev":
		c.TouchDev = v
	case "pen_touch_min":
		if n, err := strconv.Atoi(v); err == nil {
			c.PenTouchMin = n
		}
	case "input_debug":
		c.InputDebug = truthy
	case "wake_gpio":
		c.WakeGPIO = truthy
	case "state_extended":
		c.StateExtended = v
	case "oracle_key":
		c.OracleKey = v
	case "oracle_base":
		c.OracleBase = strings.TrimRight(v, "/")
	case "oracle_model":
		c.OracleModel = v
	case "oracle_reasoning":
		c.OracleReasoning = v
	case "oracle_max_tokens":
		if n, err := strconv.Atoi(v); err == nil {
			c.OracleMaxTokens = n
		}
	case "memory":
		c.MemoryOff = v == "off" || v == "0" || v == "no" || v == "false"
	case "memory_dir":
		c.MemoryDir = v
	case "memory_turns":
		if n, err := strconv.Atoi(v); err == nil {
			c.MemoryTurns = n
		}
	case "tz_offset":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.TZOffsetHours = f
		}
	case "deadman_min":
		if n, err := strconv.Atoi(v); err == nil {
			c.DeadmanMin = n
		}
	case "keep_page":
		c.KeepPage = truthy
	}
}
