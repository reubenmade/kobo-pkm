# Architecture — article store, notes store, on-device/server split

Working notes from a 2026-07-14 design pass, prompted by three open
questions: whether Instapaper stays the article store, where notes/
Ponderings should actually live long-term (listen-later's own DB vs.
Obsidian/Logseq vs. a tool like Hermes/OpenClaw), and whether the device
should ever talk to OpenAI directly or only ever to listen-later.

I don't have firsthand specifics on Hermes/OpenClaw — reasoning about it
below as "a self-hosted PKM tool," not asserting features I haven't
verified. Worth filling in what it actually offers (API? plain files?
sync model?) before betting on it.

---

## 1. Article store: keep Instapaper, but only as a clipper

Instapaper's strength is capture, not storage: the web clipper, share
sheet, and paywall/readability parsing across arbitrary sites are mature
and not worth rebuilding. Its weakness for this project is that its API is
a plain poll-based bookmarks/text API with no support for the kind of
structured, paragraph-addressed annotation data this whole system is built
around (margin widths, stroke series, GPT glosses, Pondering links) — so it
can only ever be a source of article *content*, never the annotation
store.

**Recommendation**: pull full text via Instapaper's API at "add to queue"
time, and from that point treat listen-later's own copy as the article of
record — don't re-query Instapaper for that article again. This mirrors
the "room-temperature inbox" philosophy already in `FIELD-NOTES.md`:
listen-later already has to own per-item data for the audio side, so
owning a pulled copy of the text is the same pattern, not a new one.

Net effect: Instapaper is upstream of listen-later, not a peer store next
to it. listen-later's role shifts from "maybe write our own clipper" to
"ingest from Instapaper's API," which is considerably less work.

---

## 2. Notes/Pondering store: don't pick prematurely — separate system-of-record from human view

The real fork: a bespoke store inside listen-later (SQLite, per
`CLAUDE.md`/`models.py`) vs. a markdown-file PKM tool (Obsidian, Logseq, or
Hermes/OpenClaw if it turns out to fit that shape).

Obsidian and Logseq's strength is the human-facing side — graph view,
backlinks, mature plugin ecosystems, and markdown files you own outright
with no lock-in. Their weakness here is that neither has a first-class
remote-write API built for a device pushing structured sync data:
Obsidian needs a community plugin (e.g. Local REST API) to accept writes
from outside its own app, and Logseq's canonical form is its own
markdown-graph files, normally synced via its own sync service or a
folder-sync tool (Syncthing, git) rather than an inbound API. Wiring the
Kobo's sync queue directly into either one means either writing markdown
files straight into their vault folder, or going through a plugin API —
both workable, but both couple the reliability of *device sync* to the
stability of a third-party app's file format and plugin surface.

**Recommendation**: decouple the two concerns.
- **System of record** = listen-later's own DB. It already owns the sync
  pipeline (`data/queue/` draining to `POST /api/kobo/annotations`, per
  `FIELD-NOTES.md` §7), already has the device's trust for durability, and
  needs to hold the raw material (stroke series, margin widths, gloss
  text, touch timestamps) regardless of what renders it for humans.
- **Human PKM view** = a one-way export/mirror job that writes markdown
  into an Obsidian or Logseq vault (or Hermes/OpenClaw, once its actual
  write path is known) on some cadence. This gets you the graph/backlink
  UX without making the Kobo's sync depend on a vault's file format
  staying put.
- Two-way sync (editing a Pondering in Obsidian and having it flow back to
  the device) is a real future want but adds real conflict-resolution
  cost — don't build it until the one-way export has been lived with.

This also matters for scope: `CLAUDE.md` currently scopes listen-later
specifically as the audio-download service. Growing its DB into the
system-of-record for notes/annotations is a deliberate scope expansion —
worth deciding explicitly whether that lives inside the existing Flask app
or as a sibling service, rather than it happening by accretion.

---

## 3. On-device vs. server-side storage

Keep the pattern `app/store.go` already established: local JSON + an
append-only queue, device never blocks on network, syncs deltas at wake.
`FIELD-NOTES.md`'s "room-temperature inbox" framing — sync is queued and
occasional, the device is structurally incapable of interrupting you —
should hold for every content type added here, not just highlights.

**Must stay on-device until synced**: raw stroke series (durability
matters — FAT loses unsynced writes on a hard restart, per the field
notes, so flush cadence is load-bearing), and whatever's needed to render
the current reading/Pondering set offline.

**Must never live only on-device**: GPT-4o glosses and summaries (need
real compute + API key custody — see §4), and the canonical Pondering
timeline (merging touches across sessions, decay logic — this is server
bookkeeping, not something a single device's local state should own).

---

## 4. Device talks only to listen-later, not directly to OpenAI

Recommendation: the device never holds an OpenAI key or calls OpenAI
directly; listen-later proxies every model call.

- **Key custody**: a device that gets left on a shelf or lost is a worse
  place to keep a live API key than a single server-held key. The riddle
  experiment's `config.txt oracle_key` is a fine shortcut for a
  throwaway pen-interaction testbed; it's not a pattern to carry into the
  real architecture.
- **Offline-first stays true**: a direct device→OpenAI call mid-session
  reintroduces exactly the "live" interruption FIELD-NOTES.md designed
  away from. Keeping all model calls server-side, batched at sync, is what
  makes "nothing is ever live" actually hold.
- **Iteration cost**: prompt tuning, retries, batching, and cost control
  are all far easier to manage in one server codebase than in a
  cross-compiled Go binary that has to be redeployed over USB to change.

This is also just the natural extension of what `FIELD-NOTES.md` §7
already plans ("server-side handwriting-to-text over stroke JSON; typed
notes flow back") — applying the same rule to vision glosses and Pondering
summarization, not a new principle.

---

## Put together

```
Instapaper (clip)
   → listen-later ingest (pull via API, own the copy from here on)
   → Kobo (render, capture ink/margin/touches into local queue)
   → sync
   → listen-later server (GPT-4o vision/text calls, Pondering timeline
     + decay logic, canonical DB)
   → [optional] one-way export
   → Obsidian/Logseq/Hermes vault (human browsing, backlinks, graph view)
```

The existing audio-download pipeline in listen-later is unaffected by any
of this — it's additive, not a replacement of what's there today.

**Open questions to resolve before committing further**:
- Instapaper API's actual rate limits / text-extraction quality — worth a
  quick spike before assuming it's sufficient for every source.
- What Hermes/OpenClaw actually offers for inbound writes, if it's still a
  live candidate.
- Whether the notes system-of-record lives inside listen-later's existing
  Flask app/SQLite or as a new sibling service — a scope decision, not a
  technical one.
