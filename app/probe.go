//go:build linux && !sim

package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// RunProbe is a one-shot display diagnostic: fill the ENTIRE framebuffer
// memory with black or white (bypassing all layout math — 0x00/0xff are
// black/white in every pixel format), then try each e-ink update variant
// in turn. The user watches for flashes; the log records which variants
// the kernel accepted and how long each ioctl took.
func RunProbe(f *FB) {
	full := image.Rect(0, 0, f.fbW, f.fbH)
	transposed := image.Rect(0, 0, f.fbH, f.fbW)
	half := image.Rect(0, 0, f.fbW, f.fbH/2)

	fill := func(v byte) {
		for i := range f.mem {
			f.mem[i] = v
		}
	}

	type step struct {
		desc   string
		fill   byte
		v1     bool
		region image.Rectangle
		wf, um uint32
		temp   int32
	}
	steps := []step{
		{"v2 GC16 FULL ambient portrait-region BLACK", 0x00, false, full, wfModeGC16, updateModeFull, tempUseAmbient},
		{"v2 GC16 FULL ambient portrait-region WHITE", 0xff, false, full, wfModeGC16, updateModeFull, tempUseAmbient},
		{"v2 GC16 FULL temp=24 BLACK", 0x00, false, full, wfModeGC16, updateModeFull, 24},
		{"v2 GC16 FULL transposed-region WHITE", 0xff, false, transposed, wfModeGC16, updateModeFull, tempUseAmbient},
		{"v1 GC16 FULL ambient BLACK", 0x00, true, full, wfModeGC16, updateModeFull, tempUseAmbient},
		{"v1 GC16 FULL transposed-region WHITE", 0xff, true, transposed, wfModeGC16, updateModeFull, tempUseAmbient},
		{"v2 AUTO PARTIAL half-screen BLACK", 0x00, false, half, wfModeAuto, updateModePartial, tempUseAmbient},
		{"v2 DU PARTIAL full WHITE", 0xff, false, full, wfModeDU, updateModePartial, tempUseAmbient},
	}

	// Count every signal the process receives so the EINTR source is named.
	var mu sync.Mutex
	counts := map[string]int{}
	sigc := make(chan os.Signal, 1024)
	signal.Notify(sigc)
	go func() {
		for s := range sigc {
			mu.Lock()
			counts[s.String()]++
			mu.Unlock()
		}
	}()
	sigReport := func() string {
		mu.Lock()
		defer mu.Unlock()
		var keys []string
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := ""
		for _, k := range keys {
			out += fmt.Sprintf("%s=%d ", k, counts[k])
		}
		return out
	}

	log.Printf("probe: starting %d steps — watch the screen", len(steps))
	for i, s := range steps {
		fill(s.fill)
		f.marker++
		start := time.Now()
		var err error
		if s.v1 {
			err = f.sendV1(s.region, s.wf, s.um, s.temp)
		} else {
			err = f.sendV2(s.region, s.wf, s.um, s.temp)
		}
		log.Printf("probe %d/%d: %s -> err=%v elapsed=%s signals[%s]", i+1, len(steps), s.desc, err, time.Since(start), sigReport())
		time.Sleep(1500 * time.Millisecond)
	}

	// Final step: same v2 GC16 FULL update, but issued from a dedicated OS
	// thread with all noise signals blocked — if plain attempts fail with
	// EINTR and this succeeds, runtime signals were the culprit.
	fill(0x00)
	f.marker++
	start := time.Now()
	err := ioctlOnMaskedThread(func() error {
		return f.sendV2(full, wfModeGC16, updateModeFull, tempUseAmbient)
	})
	log.Printf("probe masked-thread: v2 GC16 FULL BLACK -> err=%v elapsed=%s signals[%s]", err, time.Since(start), sigReport())
	time.Sleep(1500 * time.Millisecond)
	log.Printf("probe: done")
}

// ioctlOnMaskedThread runs fn on a locked OS thread whose signal mask blocks
// everything non-fatal (SIGHUP, USR1/2, PIPE, ALRM, CHLD, URG, VTALRM, PROF,
// WINCH, IO). The thread is discarded afterwards.
func ioctlOnMaskedThread(fn func() error) error {
	res := make(chan error, 1)
	go func() {
		runtime.LockOSThread() // never unlocked: thread dies with goroutine
		var mask [2]uint32
		for _, s := range []uint32{1, 10, 12, 13, 14, 17, 23, 26, 27, 28, 29} {
			mask[(s-1)/32] |= 1 << ((s - 1) % 32)
		}
		syscall.Syscall6(syscall.SYS_RT_SIGPROCMASK, 0 /*SIG_BLOCK*/, uintptr(unsafe.Pointer(&mask[0])), 0, 8, 0, 0)
		res <- fn()
	}()
	return <-res
}
