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

Also: **`experiments/riddle/`** — a full Go port of the reMarkable "Tom
Riddle diary" (pen → vision LLM → animated handwritten reply). It's the
pen-interaction testbed: pressure-width ink, hover, eraser, side button,
sleep cover, and the stylus findings in §2 below all came out of it.

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

### Stylus, decoded (riddle experiment, 2026-07-14)

Everything below is empirical, from frame-level evdev logging of the elan
touchscreen with the Kobo Stylus 2 (`experiments/riddle/`, logs archived on
device as `log-session*.txt`).

- **The digitizer streams the pen while it HOVERS.** The MT tracking ID stays
  alive the whole time the pen is in range — treating "tracked" as "touching"
  inks connecting lines between strokes. **Contact = pressure ≥ threshold,
  nothing else.** (~30 of 4095 works; pressure arrives one frame after
  tracking starts, so the first frame of a real touch reads p=0.)
- **BTN_TOUCH is a trap:** the elan holds BTN_TOUCH=1 for the entire
  pen-in-range period, hover included. It must never vouch for contact.
  (This mistake shipped twice before the frame log caught it.)
- **Hover is a real, usable input channel**: position streams at full rate
  with p=0 and `ABS_MT_DISTANCE`=15 (contact reads d=0). Free primitive for
  hover cursors / reveal-on-approach. Key **236** pulses as a pen-detect
  (in-range) signal alongside it.
- **Pen buttons:** key **331** (BTN_STYLUS) = the tail **eraser**; key
  **332** (BTN_STYLUS2) = the **side button**. Clean 1/0 press/release on
  both. BTN_TOOL_RUBBER never fires. (Don't infer the mapping from log
  timing patterns — verify on device; we had it backwards once.)
- **Pressure is honest and useful**: 0..4095, smooth ramps, good enough to
  drive stroke width directly. **Fingers report real pressure too** (~2000
  raw), so one pressure gate serves pen and finger alike, and
  `ABS_MT_TOOL_TYPE` distinguishes them (0=finger, 1=pen).
- Also present, unexplored: tilt axes 26/27 (always 0 so far — may need
  angle), TOUCH_MAJOR ≈539 for the pen tip, and an undecoded Elan-specific
  axis pair 41/42.
- Page-turn buttons **confirmed grabbable with Nickel dead** (gpio-keys
  193/194, EVIOCGRAB succeeds) — the takeover fork works as planned.
- Sleep inputs: power button = key **116**, sleep-cover hall sensor = keys
  **35/59** (pwrkey/gpio devices).
- **Suspend/wake on the MTK kernel: WORKING, after three mis-diagnoses.**
  The write to `/sys/power/state` BLOCKS for the entire sleep and returns
  nil only after resume — **that return IS the wake signal**. This kernel
  has NO `/sys/power/suspend_stats`, so polling a success counter reads 0
  forever and labels real sleeps as failures (an 83s successful
  suspend+power-wake was dismissed as "aborted" this way). Cover-open AND
  power-button wake both work with the default wakeup arming (platform
  gpio-keys ships enabled; don't touch it — disarming it was another wrong
  turn, and enableWakeup on the input-class node broke waking entirely
  once). Historical detail of the wrong turns below:
- **Suspend prerequisites on the MTK kernel (the parts that were right):** direct
  `mem > /sys/power/state` writes EPERM even with `autosleep=off` and
  `state-extended=1`. KOReader's discipline (device.lua) is what's missing:
  **kill Wi-Fi before every suspend** (the wmt driver blocks suspend while
  the radio is up — every EPERM session here had wlan0 up), and **never
  suspend while plugged in/charging — it hangs the MTK kernel**. Full
  sequence: skip if charging; frontlight → 0 (independently powered LED);
  wifi down (`ifconfig wlan0 down` + `0 > /dev/wmtWifi`);
  `1 > /sys/power/state-extended`; settle ~2s + sync; `mem >
  /sys/power/state` confirmed via `/sys/power/suspend_stats/success` with
  retries; reverse everything on wake. **Never trust a sleep card — verify
  via suspend_stats/success** (a "sleeping" device may be awake and
  retrying). A startup `pmProbe` logs the whole /sys/power layout.
- **The Kobo has no CA root store where Go looks** — any HTTPS call from a
  Go binary fails `x509: certificate signed by unknown authority` (Wi-Fi
  can be perfectly healthy; check operstate/carrier before blaming the
  network). Fix: `import _ "golang.org/x/crypto/x509roots/fallback"`
  (embedded Mozilla roots, ~300KB), or ship a bundle + SSL_CERT_FILE.
- **Nickel powers the Wi-Fi chip down as it dies** — a takeover app that
  needs network must revive it: `1 > /dev/wmtWifi`, `ifconfig wlan0 up`,
  wpa_supplicant against Nickel's own `/etc/wpa_supplicant/wpa_supplicant.conf`
  (it maintains known networks there — KOReader leans on the same file),
  then dhcpcd. Cover close = key 35 on gpio-keys (autorepeats while
  closed); key 59 possibly the open edge (undecoded).
- **Wake sources are per-device and mostly disarmed**: the power button's
  PMIC (`bd71828-pwrkey`) wakes the system out of the box, but the sleep
  cover's hall sensor (gpio-keys) ships with `power/wakeup` disabled —
  Nickel arms it at sleep time. Write `enabled` to
  `/sys/class/input/inputN/device/power/wakeup` for the gpio-keys device or
  only the power button wakes a suspended takeover app.
- **A "spontaneous reboot" may be your own recovery code**: what looked
  like suspend crash-rebooting the device (boot animation on wake!) was the
  app's stall watchdog exiting during slow suspend retries — run.sh's
  Nickel restart plays `on-animator.sh`, i.e. the boot spinner. Any
  watchdog must be fed by (or disarmed around) intentionally-blocking paths
  like suspend, and "reboot symptoms" deserve a log check before kernel
  theories.

### Nickel-takeover hazards (each cost us a session)

- **NickelMenu "disappears" if Nickel dies (or the device hard-reboots)
  within ~20s of a Nickel start.** It's not an uninstall: NM's failsafe
  RENAMES its library (`/usr/local/Kobo/imageformats/libnm.so` →
  `libnm.so.failsafe`) on every start and renames it back ~20s later — die
  inside the window and the lib stays stranded under the wrong name, so NM
  never loads again (`.adds/nm/` config survives, which makes it look
  spooky). Four losses before the defenses were complete. Three layers now:
  run.sh waits for `libnm.so.failsafe` to vanish before the killall;
  restart-nickel.sh heals a stranded rename before starting Nickel; and —
  the one that closes the loop — a **boot-time udev heal** on the rootfs
  (`/etc/udev/rules.d/98-nm-heal.rules` → `/usr/local/nm-heal/heal.sh`,
  fires on loop0 like kfmon, before Nickel), so ANY power cut self-repairs
  on the next boot. Installer with NM + heal bundled:
  `vendor/KoboRoot-nickelmenu-v0.6.0+heal.tgz` → `.kobo/KoboRoot.tgz`, eject.
- **Don't take over right after a USB eject** — Nickel is mid content-import
  and killing it there (suspected) hard-wedged the device once. Wait ~30s.
- **Assume the log dies with the device**: FAT loses unsynced writes on hard
  restart — the wedged session left literally nothing, not even run.sh's
  first echo. Fsync the log every ~2s, and run an in-app stall watchdog that
  dumps all goroutine stacks to the log before exiting (a wedge with input
  grabbed is indistinguishable from a kernel hang otherwise).
- A takeover app that needs network must **spare dhcpcd** in the kill list
  and have Wi-Fi up before launch — with Nickel dead nothing can raise it.

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
