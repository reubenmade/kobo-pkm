//go:build linux && !sim

package main

import (
	"image"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	base := "/mnt/onboard/.adds/listenlater"
	probe := false
	for _, arg := range os.Args[1:] {
		if arg == "--probe" {
			probe = true
		} else {
			base = arg
		}
	}
	os.MkdirAll(base, 0755)
	lf, err := os.OpenFile(filepath.Join(base, "log.txt"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(lf)
	}
	log.Printf("---- listenlater starting ----")

	cfg := LoadConfig(filepath.Join(base, "config.txt"))
	InitFonts()

	fb, err := OpenFB(cfg)
	if err != nil {
		log.Fatalf("fb: %v", err)
	}
	defer fb.Close()

	if probe {
		// Diagnostic mode: never grabs input, exits on its own.
		RunProbe(fb)
		return
	}

	store := OpenStore(filepath.Join(base, "data"))
	app := NewApp(fb, store)
	app.Push(NewDashboard()) // Push renders + refreshes

	// Paint BEFORE grabbing input: if the display path is broken the user
	// still controls the device — no forced reboot needed.
	log.Printf("first paint done, grabbing input")

	touch, err := OpenTouch(cfg, fb.Bounds().Dx(), fb.Bounds().Dy())
	if err != nil {
		log.Fatalf("touch: %v", err)
	}
	defer touch.Ungrab()

	// Physical page-turn buttons (optional — not all models have them).
	keys, err := OpenKeys()
	if err != nil {
		log.Printf("keys: %v (physical buttons disabled)", err)
	} else {
		defer keys.Ungrab()
		go keys.Run(app.Keys)
	}

	// Keep rotation events away from Nickel so it doesn't repaint over us.
	if release, err := GrabDevice("accel"); err == nil {
		defer release()
	} else {
		log.Printf("input: %v", err)
	}

	// The Libra Colour's page buttons haven't shown up on gpio-keys —
	// watch the PMIC key device too until we find them.
	SpyDevice("pwrkey", app.Keys)

	// exitClean shows a visible confirmation, releases input, and exits —
	// used by every exit path so the user always knows the app is gone.
	exitClean := func(why string) {
		log.Printf("exit: %s", why)
		c := fb.Canvas()
		FillRect(c, fb.Bounds(), 255)
		DrawStringTop(c, H1, "Listen Later closed", 80, fb.Bounds().Dy()/2-80, 0)
		DrawStringTop(c, Body, "press power to sleep/wake — Kobo will redraw", 80, fb.Bounds().Dy()/2+20, 0)
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

	// POC dead-man switch: never wedge the device for longer than 20 min.
	go func() {
		time.Sleep(20 * time.Minute)
		exitClean("dead-man timer")
	}()

	// Exit when a USB connection appears: Nickel draws its Connect dialog
	// over our framebuffer, so an app that keeps the input grab here just
	// looks like a dead touchscreen. Hand the device back instead.
	go func() {
		prev := usbOnline()
		for range time.Tick(3 * time.Second) {
			cur := usbOnline()
			if cur && !prev {
				exitClean("usb connected — handing Nickel the Connect dialog")
			}
			prev = cur
		}
	}()

	// Forward touches with two POC safety/debug aids:
	// - a dot at every touch-down so miscalibration is visible on screen
	// - 3 consecutive touch-downs in any screen corner exit the app; the
	//   corner set is closed under swap/mirror errors, so this works even
	//   when the axis mapping is wrong.
	raw := make(chan Touch, 64)
	go touch.Run(raw)
	go func() {
		corner := 0
		W, H := fb.Bounds().Dx(), fb.Bounds().Dy()
		for t := range raw {
			if t.Kind == TouchDown {
				dot := image.Rect(t.X-6, t.Y-6, t.X+6, t.Y+6)
				FillRect(fb.Canvas(), dot, 0)
				fb.Refresh(dot, RefreshFast)
				// Top corners only: the bottom corners host footer
				// buttons, and rage-tapping those must not exit the app.
				nearX := t.X < 160 || t.X > W-160
				nearY := t.Y < 160
				_ = H
				if nearX && nearY {
					corner++
					if corner >= 3 {
						exitClean("corner-exit")
					}
				} else {
					corner = 0
				}
			}
			app.Touches <- t
		}
		close(app.Touches)
	}()

	app.RunNoInitialRender()
	log.Printf("---- listenlater exiting ----")
	exitClean("exit button")
}
