# Kobo — reading & notes hardware end

Hardware end of the knowledge pipeline: an Instapaper-style reading surface on
the Kobo with typed + drawn highlights/notes that sync back to the listen-later
server. Layout heavy lifting happens server-side; the Kobo renders and pushes
annotations up.

## Native app (`app/`) — the real POC

A single static Go binary that takes over the device completely:

- draws straight to the e-ink framebuffer (`/dev/fb0` + `MXCFB_SEND_UPDATE`
  ioctls) — DU waveform partial refreshes for pen strokes/selection (fast),
  GC16 full flashes for page turns
- grabs the touch device (`EVIOCGRAB`) so Nickel doesn't see our input
- renders its own text with word-level hit boxes

Screens: **dashboard** (date, weather, events, todos, current reads, highlight
loop) → **reading queue** → **article** (paginated lorem ipsum with figures).

Article interactions:
- tap left/right edges to turn pages
- **drag across words** to select → action bar: plain highlight, **pen note**
  (scribble pad, strokes kept raw for server-side handwriting-to-text),
  **typed note** (on-screen keyboard)
- highlights render underlined with a margin marker when noted; tap one to
  view/delete its note

Persistence: `data/highlights.json` plus an append-only `data/queue/` of
`{op, highlight}` JSON — a future sync worker drains that to the server, the
UI never blocks on network.

### Build & deploy

```sh
./deploy-app.sh            # cross-compiles (GOOS=linux GOARM=7) + copies to /Volumes/KOBOeReader
diskutil eject /Volumes/KOBOeReader
# on the Kobo: NickelMenu (main menu) -> "Listen Later"
```

Files land in `.adds/listenlater/` on the device: `listenlater` (binary),
`run.sh` (NickelMenu launches this; logs to `log.txt`), `config.txt`.

### Simulator

The same app compiles for the desktop and runs a scripted touch scenario,
dumping PNG snapshots of every stage:

```sh
cd app && go build -tags sim -o /tmp/llsim . && /tmp/llsim /tmp/simout
```

### Device calibration

Panel rotation and touch axes vary between Kobo models. If the screen renders
rotated or taps land in the wrong place, edit `.adds/listenlater/config.txt`
(`rot`, `swap`, `mirx`, `miry`) and relaunch. `log.txt` logs the fb geometry
and every input device found. Escape hatch if touch is unusable: hold power
~15 s to reboot — Nickel comes back clean.

### Known POC gaps

- After exiting the app, Nickel doesn't know to repaint: sleep/wake the device
  or open a menu to redraw. (Fix later via NickelDBus.)
- Kobo's idle-sleep timer keeps running while the app has input grabbed — set
  a long sleep timeout while testing.
- e-ink refresh ioctl struct is probed v2→v1 at runtime; Libra 2 should take
  the v2 path (check `log.txt` if the screen never updates).

## Browser POC (`dashboard/`) — superseded

First iteration: an ES5 HTML dashboard opened in the hidden Kobo browser via
NickelMenu. Kept for reference; the native app replaces it.

- `dashboard/index.html` — the page (pen canvas + localStorage persistence)
- `install.sh` — copies it + stages NickelMenu's KoboRoot.tgz
- `vendor/KoboRoot-nickelmenu-v0.6.0.tgz` — pinned NickelMenu release

## Next steps

- Touch calibration pass on the Libra 2, then feel-test pen latency
- Sync worker: drain `data/queue/` → `POST /api/kobo/annotations` on the
  listen-later server (Wi-Fi wake is the interesting part)
- Server-rendered article payloads (title/paras/images as JSON) replacing
  the hardcoded mocks; server-side handwriting-to-text over stroke JSON
- Nickel repaint-on-exit via NickelDBus
