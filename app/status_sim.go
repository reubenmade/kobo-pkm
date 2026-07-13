//go:build sim

package main

func BatteryStatus() (int, bool)     { return 82, true }
func FrontlightPercent() (int, bool) { return 40, true }
func SetFrontlight(pct int)          {}
