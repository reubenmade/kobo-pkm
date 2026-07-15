# Tangle — reactive documents on the Kobo

Two Bret-Victor-style [Tangle](https://worrydream.com/Tangle/) one-pagers, where
the numbers in the prose are live. The first native experiment built on the
shared **`kit/`** infra layer.

- **Page 1 — Proposition 21.** A civic what-if: scrub the surcharge or the
  number of vehicles and the revenue, the leftover parks budget, and projected
  attendance all update in the sentence.
- **Page 2 — A state-variable filter** ("the electrical"). A block diagram of
  the filter (two integrators in a feedback loop, with high-/band-/low-pass
  taps) plus the three response curves. Scrub the cutoff **Fc** or the resonance
  **Q** and all three curves reshape live — the peak grows with Q, the corner
  slides with Fc.
- **Page 3 — Ten Brighter Ideas, No. 3** (efficient appliances). Scrub the
  efficiency gain and the adoption rate; energy saved (TWh), CO₂ (Mt), and money
  ($B) update as bars, and a pictograph shows how many nuclear reactors that
  displaces.

Whenever you scrub, a **popover** floats over the number showing its value and
where it sits in its range.

## The gesture

Everything is done with the pen: **hover over a bold number, hold the pen's
side button, and slide sideways.** The number scrubs, dependent numbers
recompute, and the page redraws (throttled to ≤16 Hz with the fast DU waveform
so it stays smooth on e-ink; one clean settling pass when you let go). No
contact needed — the elan streams the hovering pen, and the scrub rides the
side button (BTN_STYLUS2), exactly the channel decoded in the riddle experiment.

- **Physical page buttons** flip between the two pages.
- **3 taps in a top corner** exits (works even if touch calibration is off).
- **Power button / cover** sleeps and wakes, restoring the page.

## Build & verify off-device (do this first)

```
go build -tags sim -o build/tangle-sim .
./build/tangle-sim simout        # writes PNG snapshots of every state
```

`simout/` shows each page, a hover, and a full scrub of every number — the whole
interaction without a Kobo.

## Deploy

Plug in the Kobo, tap Connect, then:

```
./deploy.sh
diskutil eject /Volumes/KOBOeReader
```

Wait ~30s after eject (Nickel is mid content-import), then launch **Tangle**
from NickelMenu. `deploy.sh` cross-compiles the ARM binary and generates the
Nickel-takeover scripts from `kit/scripts/*.tmpl`.

## How it's put together

- `tangle.go` — the reactive-document model: `Var` (a scrubbable number),
  inline token flow that lays prose and records each number's hit box, the two
  pages' render functions, the response-curve plot, and the hover/scrub
  interaction with throttled dirty-region refresh. Portable (no build tags), so
  the sim and the device share it exactly.
- `main_device.go` — wires `Doc` into `kit.Handler` and calls `kit.Run`. The kit
  owns the framebuffer, grabbed input, watchdog, corner-exit, USB-exit, and
  suspend; this file is ~60 lines.
- `main_sim.go` — drives `Doc` against `kit.SimDisplay`, simulating pen-button
  scrubs and snapshotting each state.

To build a new experiment, copy this shape: implement `kit.Handler`, and you get
the whole takeover lifecycle for free.
