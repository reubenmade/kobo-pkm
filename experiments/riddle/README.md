# riddle on Kobo — the diary of Tom Riddle, ported to the Libra Colour

A Go port of [MaximeRivest/riddle](https://github.com/MaximeRivest/riddle)
(reMarkable Paper Pro, Rust) onto the kobo-pkm native stack. Write on the
page; after a pause the diary **drinks your ink** — your words dissolve into
the paper — the page thinks, and an answer writes itself back in a flowing
hand, stroke by stroke, then fades away.

Feature parity with upstream: streamed sentence-by-sentence replies,
handwriting synthesis (Dancing Script → Zhang-Suen skeleton → traced strokes
→ animated replay), diary memory with conjured pages ("show me what I wrote
about…" rewrites the remembered page in your own hand, dated, in faded ink),
the big-"?" guide, and power-button sleep.

## What replaced what

| riddle (reMarkable, Rust)               | here (Kobo, Go)                          |
|-----------------------------------------|------------------------------------------|
| quill/qtfb display backends             | hwtcon framebuffer ioctls (kobo-pkm)     |
| raw Elan *marker* digitizer, 4k pressure| elan touchscreen — pen + finger share it |
| flip-the-marker eraser                  | BTN_TOOL_RUBBER honored if ever seen (logged) |
| 5-finger-tap quit                       | 3 taps in a top corner                   |
| xochitl stopped by AppLoad script       | Nickel killed by run.sh, revived on exit |
| oracle.env                              | config.txt (env vars still override)     |
| pi RPC backend                          | not ported (HTTP + fake backends only)   |

## Running it

```sh
./deploy.sh            # Kobo USB-mounted; installs to .adds/riddle + NickelMenu entry
```

1. Put an `oracle_key` in `.adds/riddle/config.txt` (ships with `fake`,
   which streams a canned reply — the whole interaction loop works offline).
2. **Connect Wi-Fi before launching** — run.sh spares dhcpcd, but nothing
   can bring Wi-Fi up once Nickel is dead.
3. NickelMenu → **The Diary**. Write. Rest the pen.

Exits: 3 taps in a top corner · plugging in USB · `deadman_min` (default 60)
· SIGTERM. Every path restores Nickel. If anything ever wedges: hold the
power button to hard-reboot.

## Simulator

```sh
go build -tags sim -o build/riddle-sim . && ./build/riddle-sim simout
open simout/*.png
```

Runs the real state machine against a PNG display, a fake clock, and the
fake oracle: written page → drinking → reply writing itself → fade → the
"?" guide → a conjured memory. `go test ./...` covers the parser, ink
model, handwriting pipeline, memory store, and ?-detector (ported from
upstream's Rust tests).

## Interaction-hacking notes

- `diary.go` is the whole interaction loop — states, timings (idle commit
  2.8s, fade stages, linger), animation budgets. Start there.
- E-ink pacing: ink + animations go out on the A2 pen waveform, coalesced
  at ≤16Hz (`flushEvery`); faded/conjured ink needs grayscale so it rides
  AUTO; page transitions flash GC16.
- Unknown input key codes are logged (first 60) — that's how we'll find
  the page-button codes and whether the Kobo stylus eraser reports at all.
- The committed page PNG is what the oracle sees: `keep_page = 1` keeps it
  at `/tmp/riddle-page.png` for inspection.

Fonts: Dancing Script (SIL OFL 1.1, `fonts/OFL.txt`) — same reply hand as
upstream.
