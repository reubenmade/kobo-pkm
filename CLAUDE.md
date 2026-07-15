# kobo-pkm — Claude Code context

Native Go apps for a **Kobo Libra Colour** e-reader: a reading/annotation POC
(`app/`) and a series of pen-interaction experiments (`experiments/`). Everything
is a single static ARM binary that takes over the device (framebuffer + input),
launched from NickelMenu, restoring stock Nickel on exit.

## Read these first

- **`FIELD-NOTES.md`** — every hard-won device fact (display driver, waveforms,
  stylus protocol, suspend ritual, NickelMenu hazards). If something about the
  hardware surprises you, the answer is almost certainly already in here.
- **`INTERACTION-PATTERNS.md`** — the navigation model and the interaction ideas
  (Ponderings, margin-boosting, gloss cards) and how to build them.
- **`ARCHITECTURE.md`** — where notes/annotations should live (device vs
  listen-later server vs PKM vault).

## Layout

```
kit/                     shared infra — the layer new experiments build on
  draw.go   text.go   ink.go   bbox.go   config.go   display.go   (portable)
  fb_linux.go   input_linux.go   power_linux.go   app_linux.go     (device)
  sim.go                                                            (-tags sim)
  scripts/  run.sh.tmpl  restart-nickel.sh.tmpl                     (takeover)
experiments/
  tangle/   Bret Victor Tangle-style reactive docs (pen-scrub) — uses kit
  riddle/   Tom Riddle diary port (own module, predates kit)
app/         reading/annotation POC (own module, predates kit)
vendor/      fbink/fbdepth binaries + NickelMenu installers (incl. boot-heal)
```

`kit/`, `experiments/tangle/`, and the repo root are **one Go module**
(`github.com/reubenmade/kobo-pkm`). `app/` and `experiments/riddle/` each have
their **own** go.mod and are carved out of that module — they came before the
kit and haven't been ported to it. New experiments should import the kit.

## The kit (`kit/`)

The best-of device + drawing layer, extracted from `app/` and `riddle`. A new
experiment implements one interface and gets the whole takeover loop for free:

```go
type Handler interface {
    Start()                    // first paint
    Touch(t kit.Touch)         // pen/finger (pressure-gated; .Button = pen side btn)
    Key(code int)              // physical buttons (193/194 = page turn)
    Step()                     // per-tick, for animation
    SleepScreen(c *image.RGBA) // drawn before suspend
    ExitScreen(c *image.RGBA)  // the "app closed" splash
}
kit.Run(cfg, base, func(rt *kit.Runtime) Handler { ... })
```

`kit.Run` owns: framebuffer open, input grab, the accelerometer drain, the stall
watchdog, corner-exit (3 taps top corner), USB-connect exit, power-button/cover
→ suspend, and the Nickel-safe exit splash. Handlers draw into `rt.Canvas()` and
call `rt.Refresh(rect, mode)` themselves (partial refreshes are the whole game on
e-ink — see below). Suspend save/restore is automatic; the handler only paints
the sleep screen.

What lives in the kit:
- **Display** — `RefreshMode` (Fast=DU, Auto, Full=GC16 flash, Pen=A2 force),
  the hwtcon 36-byte ioctl, RGBA→panel blit with rotation.
- **Input** — full MT-slot evdev parse; **contact is pressure-gated, never
  BTN_TOUCH**; hover streams as `TouchHover`; pen side button → `Touch.Button`;
  `KeyReader` for physical buttons.
- **Power** — the MTK suspend ritual (wifi down, `state-extended`, disarm
  autosleep, blocking `mem` write whose return IS the wake signal), frontlight,
  charging guard. Don't reinvent this; it cost days.
- **Draw** — mono primitives, `BBox` dirty-region, `Ink` pen-stroke capture /
  erase / dissolve.
- **Text** — legible Go-font faces (`kit.Body/H1/Bold/...`) + word layout, for
  reactive prose. (riddle's Dancing-Script handwriting rendering stayed in
  riddle; it's reply-animation, not a general primitive.)
- **Config** — device/calibration/sleep keys, plus a `cfg.Extra` map for
  experiment-specific keys parsed from the same `config.txt`.
- **Sim** — `-tags sim` renders to PNGs. **Build every state in the sim before
  touching hardware** — it caught nearly every bug; on-device debugging is a
  slow plug/eject/launch cycle.

## Golden rules (all earned the hard way — details in FIELD-NOTES.md)

- **Never issue an e-ink update per input event.** Draw into the canvas freely;
  Refresh a bounded rect ≤16 Hz with DU/A2; one Auto/Full settling pass on
  gesture end. Unthrottled refreshes queue *seconds* of latency.
- **Contact = pressure ≥ threshold.** The elan holds BTN_TOUCH and keeps the
  tracking ID alive while the pen merely hovers.
- **Never SIGSTOP nickel** (watchdog reboot). The takeover *kills* it and
  restarts it on exit — `run.sh` + `restart-nickel.sh` handle the NickelMenu
  failsafe race; don't kill nickel within ~20s of a Nickel start.
- **Suspend only when not charging, wifi down first**, and trust the blocking
  `mem` write's return, not any success counter (this kernel has none).
- **FAT loses unsynced writes on a hard restart** — the log is fsync'd every 2s
  and is the only debugger. A wedge with input grabbed looks like a kernel hang.

## Deploy loop (device)

Plug in the Kobo, tap Connect, then from an experiment dir:

```
./deploy.sh                 # cross-compiles ARM, copies to .adds/<name>, adds NM entry
diskutil eject /Volumes/KOBOeReader
```

Wait ~30s after eject (Nickel is mid content-import), then launch from
NickelMenu. Connect Wi-Fi *before* launch if the app needs network (with Nickel
dead nothing else can raise the radio).

Cross-compile target: `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0`.

## Off-device verification (do this first, always)

```
go build -tags sim -o build/<name>-sim ./...   # or from the experiment dir
./build/<name>-sim simout                       # writes PNG snapshots
go test ./...
```
