//go:build sim

package main

import (
	"log"
	"math"
	"os"
	"time"
)

// Simulator: runs the real diary state machine against a PNG display, a
// fake clock, and the fake oracle. Every state gets a snapshot, so the whole
// interaction loop can be eyeballed without a Kobo.

var simNow = time.Unix(1783467000, 0) // 2026-07-06, a summer evening

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--oracle-test" {
		os.Exit(oracleTest())
	}
	out := "simout"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	os.RemoveAll(out)
	InitFonts()

	d := NewSimDisplay(out, 1264, 1680) // Libra Colour panel
	cfg := LoadConfig("nonexistent")
	cfg.KeepPage = true
	cfg.MemoryDir = out + "/memories"
	store := OpenMemory(cfg, out)
	oracle := &FakeOracle{Delay: 30 * time.Millisecond}

	diary := NewDiary(d, cfg, oracle, store, out+"/page.png")
	diary.now = func() time.Time { return simNow }

	// run steps the diary with the fake clock until cond holds (or gives up).
	run := func(name string, cond func() bool) {
		for i := 0; i < 5000; i++ {
			diary.Step()
			simNow = simNow.Add(20 * time.Millisecond)
			if oracleBusy(diary) {
				time.Sleep(time.Millisecond) // let the fake oracle stream
			}
			if cond() {
				return
			}
		}
		log.Fatalf("sim: %s never happened (state=%d)", name, diary.state)
	}
	inState := func(s dstate) func() bool { return func() bool { return diary.state == s } }

	// -- Turn one: write, drink, reply, fade -----------------------------
	scribble(diary, 200, 300)
	scribble(diary, 200, 420)
	d.Snap("written")

	simNow = simNow.Add(3 * time.Second) // the pen rests
	run("drinking", inState(stDrinking))
	for diary.state == stDrinking && diary.stage < 7 {
		diary.Step()
		simNow = simNow.Add(20 * time.Millisecond)
	}
	d.Snap("drinking")
	run("thinking", inState(stThinking))
	run("replying", inState(stReplying))
	for i := 0; i < 40 && diary.state == stReplying; i++ {
		diary.Step()
		simNow = simNow.Add(20 * time.Millisecond)
	}
	d.Snap("reply-writing")
	run("lingering", inState(stLingering))
	d.Snap("reply-complete")

	simNow = simNow.Add(25 * time.Second) // the reply rests, then fades
	run("fading", inState(stFadingReply))
	for diary.state == stFadingReply && diary.stage < 5 {
		diary.Step()
		simNow = simNow.Add(20 * time.Millisecond)
	}
	d.Snap("reply-fading")
	run("listening again", inState(stListening))
	d.Snap("blank-again")

	if len(store.Entries) != 1 {
		log.Fatalf("sim: expected 1 remembered page, have %d", len(store.Entries))
	}
	log.Printf("sim: memory holds %q -> %q", store.Entries[0].Transcript, store.Entries[0].Reply)

	// -- The guide: draw a big "?" ---------------------------------------
	questionMark(diary, 500, 500, 1.6)
	simNow = simNow.Add(3 * time.Second)
	run("help", inState(stHelp))
	d.Snap("guide")
	tap(diary, 600, 1500)
	run("guide dismissed", inState(stListening))
	d.Snap("guide-dismissed")

	// -- Turn two: the fake oracle conjures the remembered page ----------
	scribble(diary, 300, 700)
	simNow = simNow.Add(3 * time.Second)
	run("conjuring", inState(stConjuring))
	run("memory shown", inState(stMemoryShown))
	d.Snap("conjured-memory")
	tap(diary, 600, 900)
	run("today returns", inState(stListening))
	d.Snap("today-back")

	log.Printf("sim: done — %d snapshots in %s", d.n, out)
}

// oracleTest verifies key + endpoint + model from the dev machine before
// anything touches the device (riddle upstream's --oracle-test). It renders
// a line of "handwriting" (Dancing Script reads as script to a vision
// model), sends one real turn, and prints the streamed reply.
// Config comes from RIDDLE_OPENAI_* env vars or ./config.txt.
func oracleTest() int {
	InitFonts()
	cfg := LoadConfig("config.txt")
	if cfg.OracleKey == "" || cfg.OracleKey == "fake" {
		log.Printf("oracle-test: set RIDDLE_OPENAI_KEY (or oracle_key in ./config.txt)")
		return 2
	}
	png := os.TempDir() + "/riddle-oracle-test.png"
	canvas := NewSimDisplay(os.TempDir(), 1264, 400).Canvas()
	FillRect(canvas, canvas.Bounds(), WHITE)
	RasterizeLine("Do you know anything about the Chamber of Secrets?", 64).Blit(canvas, 40, 150, BLACK)
	ink := NewInk()
	ink.BBox.Add(40, 150, 0)
	ink.BBox.Add(1200, 250, 0)
	if err := ink.ToPNG(canvas, png); err != nil {
		log.Printf("oracle-test: render: %v", err)
		return 1
	}
	oracle := NewHTTPOracle(cfg, false)
	t0 := time.Now()
	got := ""
	for ev := range oracle.Ask(png, &TurnContext{}) {
		switch ev.Kind {
		case EvInk:
			if got == "" {
				log.Printf("first chunk +%dms", time.Since(t0).Milliseconds())
			}
			got += ev.Text + " "
			log.Printf("ink: %s", ev.Text)
		case EvTranscript:
			log.Printf("transcript: %s", ev.Text)
		case EvErr:
			log.Printf("ORACLE ERROR: %s", ev.Text)
			return 1
		}
	}
	log.Printf("--- reply complete (%dms, %d chars) ---", time.Since(t0).Milliseconds(), len(got))
	if got == "" {
		return 1
	}
	return 0
}

// oracleBusy: real goroutine streams need real time while the diary waits.
func oracleBusy(diary *Diary) bool {
	return diary.state == stThinking || diary.state == stReplying || diary.state == stDrinking
}

// scribble drags a wavy line — stands in for a written word.
func scribble(diary *Diary, x, y int) {
	diary.HandleTouch(Touch{Kind: TouchDown, X: x, Y: y, Pressure: 2000})
	for i := 1; i <= 40; i++ {
		wob := (i % 7) * 8
		diary.HandleTouch(Touch{Kind: TouchMove, X: x + i*14, Y: y + wob, Pressure: 2000})
	}
	diary.HandleTouch(Touch{Kind: TouchUp, X: x + 40*14, Y: y})
	simNow = simNow.Add(200 * time.Millisecond)
}

func tap(diary *Diary, x, y int) {
	diary.HandleTouch(Touch{Kind: TouchDown, X: x, Y: y, Pressure: 2000})
	diary.HandleTouch(Touch{Kind: TouchUp, X: x, Y: y})
}

// questionMark draws a big "?": an arc sweeping over the top and curling
// back, a straight descender, and a dot (riddle's test shape).
func questionMark(diary *Diary, cx, cy int, scale float64) {
	first := true
	stroke := func(x, y int) {
		if first {
			diary.HandleTouch(Touch{Kind: TouchDown, X: x, Y: y, Pressure: 2000})
			first = false
		} else {
			diary.HandleTouch(Touch{Kind: TouchMove, X: x, Y: y, Pressure: 2000})
		}
	}
	r := 120 * scale
	for deg := 180.0; deg <= 450.0; deg += 6 {
		a := deg * math.Pi / 180
		stroke(cx+int(r*math.Cos(a)), cy+int(r*math.Sin(a)))
	}
	dx, dy := cx, cy+int(r)
	for i := 1; i <= 20; i++ {
		stroke(dx, dy+int(float64(i)*13*scale))
	}
	diary.HandleTouch(Touch{Kind: TouchUp, X: dx, Y: dy})
	// the dot
	ddy := dy + int(300*scale) + 60
	diary.HandleTouch(Touch{Kind: TouchDown, X: dx - 5, Y: ddy, Pressure: 2000})
	diary.HandleTouch(Touch{Kind: TouchMove, X: dx + 5, Y: ddy + 5, Pressure: 2000})
	diary.HandleTouch(Touch{Kind: TouchUp, X: dx, Y: ddy + 8})
	simNow = simNow.Add(200 * time.Millisecond)
}
