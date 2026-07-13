# Kobo PKM — field notes & possibility map

Everything learned building the native reading/annotation POC on a **Kobo Libra
Colour**, plus a map of what this hardware can uniquely do for a personal
knowledge management system. Written 2026-07-13 after a day of on-device
iteration.

---

## 1. What exists right now

A single static Go binary (`app/`) that fully takes over the device:

- Direct e-ink rendering with per-interaction waveform choice (instant pen ink,
  crisp page flashes)
- Dashboard → reading queue → paginated articles with colour figures
- Word-accurate highlighting: drag to select, popover with 5 colour swatches
  (instant save), pen-first notes with keyboard fallback, scrollable pen canvas
  for long notes, tap-to-edit/delete
- Colour text-background highlights; underline = un-coloured segment with a note
- Status bar (clock, battery, frontlight with tap-to-cycle brightness)
- Local JSON store + append-only `data/queue/` of `{op, highlight}` mutations —
  a future sync worker drains this to the server; pen notes keep raw stroke
  point-series for server-side handwriting-to-text
- A **simulator backend** (`go build -tags sim`): same app code renders PNGs and
  replays scripted touches — most bugs were caught here, not on hardware

Deploy loop: `./deploy-app.sh` (cross-compiles, copies over USB), eject, launch
from NickelMenu. No toolchain beyond Go on the Mac.

---

## 2. Hard-won device facts (Libra Colour, FW 4.45.23697)

### Identity trap
Device ID `…390` is the **Libra Colour** (“monza”, Mark 13, **MediaTek SoC**) —
not the Libra 2 as most ID tables suggest. Everything display-related flows
from this: it runs Kobo's MTK **hwtcon** display driver, not the NXP `mxc_epdc_fb`
that nearly all Kobo documentation/code assumes.

### Display (the big one)
- fb: 1264×1680 portrait, **32bpp RGBA** (not BGRA!), stride 5056, single
  buffer, `id="hwtcon"` in the fixed screeninfo — use that string to detect.
- E-ink updates: ioctl `0x4024462E` = `_IOW('F', 0x2E, struct hwtcon_update_data)`
  — **36 bytes**: rect(top,left,w,h) + waveform + update_mode + marker + flags +
  dither. Same command number as mxcfb but different struct size, so mxcfb
  callers get a baffling **EINTR** (not ENOTTY!) forever. Reference:
  `eink/mtk-kobo.h` in FBInk **master** (the release tarball predates Kobo MTK).
- Waveforms: DU=1, GC16=2, AUTO=257, **A2=6 + flag 0x10 (FORCE_A2_OUTPUT) is the
  dedicated pen mode** — this is what makes ink feel instant. GCC16=10 exists
  for colour images (always FULL).
- Each SEND_UPDATE ioctl costs ~100ms wall time. **Never issue one per touch
  event** — draw to your buffer freely, flush ≤16Hz, one settling AUTO pass on
  gesture end. Unthrottled per-move refreshes queue *seconds* of latency.
- Retry EINTR with a deadline; e-ink ioctls get interrupted routinely.
- FBInk (the binary from a KOReader release bundle — vendored in `vendor/`) is
  the definitive cross-check: if `fbink -c -f` paints and your code doesn't,
  your ioctls are wrong, not the device. Its startup banner also identifies the
  device and fb pixel format authoritatively.

### Nickel coexistence (chose: coexist, don't replace)
- **NEVER SIGSTOP nickel.** It feeds the hardware watchdog; freezing it reboots
  the device ~a minute later and eats your logs (FAT writes lost on power cut).
  KOReader gets away with *killing* nickel because the watchdog fd closes.
- Instead: EVIOCGRAB the touchscreen, buttons, **and accelerometer** (rotation
  events otherwise make Nickel repaint over your framebuffer).
- Nickel's idle-sleep timer keeps running and sees no input — long sessions hit
  auto-sleep unless the user sets a long timeout. (Fixable later via NickelDBus
  or by petting Nickel with synthetic input.)
- Nothing repaints the screen when you exit — paint an explicit "app closed"
  splash, and auto-exit when USB connects so the Connect dialog is reachable.
- Kobo sleep-covers: power button events are on a separate device
  (`bd71828-pwrkey`); leave it ungrabbed and hard power-off always works.

### Input
- elan touchscreen reports **1680×1264 — axes transposed from the display** and
  Y mirrored (`swap` + `miry`). Auto-detect swap by comparing aspect
  orientations; ship mirror flags in a config file; log raw+mapped coords for
  one-tap calibration.
- **Multi-touch matters even for a "single-touch" app**: fast two-thumb typing
  interleaves MT protocol-B slots; a single-contact parser drops keys. Track
  slots; treat the first contact as the gesture stream and deliver secondary
  contacts as independent taps.
- The Kobo Stylus (MPP) arrives through the same touch pipeline — taps skitter
  a few px, so gesture code needs "reached a different word" (not just a pixel
  threshold) before treating movement as a drag.
- Physical page-turn buttons: **located and confirmed unreachable while Nickel
  runs.** They are keys 193/194 (F23/F24) on `gpio-keys` — but **Nickel holds an
  EVIOCGRAB exclusive grab** on that device (our grab attempt returns EBUSY and
  an ungrabbed reader receives nothing). Getting the buttons requires the
  KOReader model: kill Nickel for the session and restart it on exit. Filed as
  a deliberate future fork, not a bug.
- Corner-exit safety gesture: corners are closed under any swap/mirror error,
  so "3 taps in a top corner exits" works even when calibration is wrong. Keep
  it away from the bottom corners — footer buttons live there.

### Toolchain & iteration
- Pure-Go, CGO-free, `GOOS=linux GOARCH=arm GOARM=7`: single ~3MB static binary,
  zero cross-toolchain pain. Fonts via `golang.org/x/image/font/gofont`
  (note: no ✎⌫⇧ glyphs — draw symbols or use ASCII).
- The **simulator backend is the highest-leverage thing in this repo**: PNG
  snapshots + scripted touches caught layout bugs, hit-box bugs, and flow bugs
  in seconds. E-ink hardware debugging is slow (plug/eject/launch cycles);
  never debug on-device what the sim can catch.
- Log *everything* to onboard flash (buttons, screens, raw input, ioctl
  results). USB mass-storage is the only remote-debug channel; the log file IS
  the debugger. But remember power loss truncates it.

---

## 3. Proven possible

- Full-screen native app coexisting with stock Nickel, launched from NickelMenu
- Colour rendering (Kaleido panel) — tints, saturated swatches, colour figures
- Pen ink with imperceptible latency (A2 + throttled flush)
- Word-level text selection & annotation with visual feedback
- Popovers/modals/on-screen keyboard — a real UI toolkit, hand-rolled
- Battery/frontlight read *and* frontlight write via sysfs
- Physical-button reading (mechanism proven; device TBD)
- Offline-first annotation queue ready for server sync

## 4. Not possible / not worth it

- **Reusing any Nickel UI** (keyboard, header, dialogs): welded into Nickel's
  Qt process. You hand-roll or you replace Nickel entirely (KOReader-style,
  loses the stock experience).
- **True per-update hardware inversion** on this panel (`canHWInvert=false`) —
  dark mode means rendering inverted pixels yourself.
- Grabbing input *selectively* (e.g. only some touches): EVIOCGRAB is all or
  nothing per device.
- Anything requiring iOS/Android-style background execution: when Nickel sleeps
  the device, everything stops. Scheduled work belongs on the server.

## 5. Possible with effort (unexplored)

- **Wi-Fi sync from inside the app**: the stack is standard Linux
  (`wpa_supplicant`); Nickel manages it normally. Bringing the radio up
  ourselves, syncing the queue, and dropping it is very doable — the polite
  version pokes NickelDBus to toggle Wi-Fi instead.
- **NickelDBus** (`qndb`): clean exit-repaint, toasts, Wi-Fi control, opening
  books. One extra KoboRoot.tgz install. Worth it once the POC stabilises.
- **Stylus depth**: hover, pressure, and the eraser end may emit distinct evdev
  codes — nobody's mapped them on Libra Colour; raw event logging would.
- **Bluetooth audio**: the Libra Colour has BT for Kobo audiobooks — a bridge to
  listen-later (play the audio queue from the same device you read on) is
  conceivable, though the audio stack is undocumented territory.
- **Custom sleep screen**: Nickel renders `.kobo/screensaver/` images on sleep.
  A sync step that drops a server-rendered image there is trivial — see §6.

## 6. PKM patterns unique to this hardware

The interesting ones — things that only make sense *because* it's e-ink, a
dedicated device, with a pen, that lives away from your phone:

**The screen that never turns off.** E-ink persists without power. Exit states
and sleep screens are *displays you keep*. Push a server-rendered daily digest
(calendar, todos, one looped highlight, weather) into the sleep-screen slot at
every sync: the device shows your day even "off" on the shelf. Zero battery
cost. Nothing with an LCD can do this.

**Editions, not feeds.** E-ink hates scrolling; pagination is native. Lean in:
the server compiles a finite *morning edition* — N articles, your highlights
queued for review, today's agenda — and it **ends**. Completable media on a
device that can't notify you. The anti-doomscroll reader.

**Write first, structure never (on device).** The pen pad already stores raw
strokes. Extend to a *global margin*: scribble on any page, anywhere, tied to
the paragraph beneath. The server's handwriting-to-text turns it into typed
notes by next sync. The device never asks you to organise anything — capture is
instant and messy; structure is the server's job overnight.

**Drawn symbols as commands.** Recognise a handful of margin glyphs
(server-side, from strokes): `?` = research this, `→` = todo, `★` = favourite,
big `X` = archive. A whole command language that only makes sense with a pen —
no menus, no chrome, marks on a page like you'd make in a real book.

**Highlight colour as a semantic channel.** The 5 colours aren't decoration:
yellow = quote for the commonplace book, green = action item (becomes a todo on
the server), blue = reference to link, pink = disagree/counter-argument, orange
= vocabulary/term. Colour picked at capture time = zero-cost tagging. The
server routes each colour differently.

**Passive spaced repetition.** Every dashboard visit (and every sleep screen)
loops one old highlight. Physical buttons (once found) become keep/next triage.
Review happens as a side effect of picking the device up — no "study session"
ever scheduled.

**Dwell as signal.** The app knows which page is showing and for how long.
Per-paragraph dwell time, uploaded with the queue, tells the server what
gripped you — feeding both the review loop and queue ordering. On a
single-purpose device, dwell is honest in a way phone telemetry never is.

**The room-temperature inbox.** Because sync is queued and occasional, nothing
is ever "live". Share-to-kobo from the listen-later pipeline lands articles at
the *next* sync. The device is structurally incapable of interrupting you —
that's the product, not a limitation.

---

## 7. Architecture next steps

1. Decide the Nickel question: coexist (current, no physical buttons) vs
   kill-and-restart Nickel per session (KOReader model — buttons, no sleep-timer
   interference, full device ownership, at the cost of ~15s Nickel restart on
   exit and owning sleep/power management ourselves)
2. Sync worker: drain `data/queue/` → `POST /api/kobo/annotations`; pull
   article JSON + dashboard data + sleep-screen image (Wi-Fi via NickelDBus)
3. Server-side handwriting-to-text over stroke JSON; typed notes flow back
4. NickelDBus install for clean exit + Wi-Fi + toasts
5. Server-rendered article payloads replace `content.go` mocks
6. Sleep-screen digest renderer (server) + sync-time copy into `.kobo/screensaver/`
