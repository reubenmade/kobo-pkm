# Interaction patterns — build thinking

Working notes from a 2026-07-14 design pass on four interaction ideas:
Ponderings, margin-boosting, paragraph-level annotation glosses, and the
category/deck/item navigation model. This doc works through how to actually
build each one against the current app (`app/`) and hardware facts in
`FIELD-NOTES.md`. Treat the navigation model as the prerequisite — the other
three are content types that live inside it.

---

## 0. Navigation model (build this first)

The shape that emerged: a recursive pager, not a menu tree.

- **Forward/back (physical buttons, `gpio-keys` 193/194)** — move laterally
  among siblings at the current depth. Never changes depth.
- **Tap** — descend into the currently-shown sibling's children, entering at
  child index 0 (or wherever last left off — see persistence below).
- **Ascend a level** — the open problem. The device has exactly two physical
  buttons, no dedicated "back" key. Options, in preference order:
  1. **Long-press either button** = ascend. Buttons are already
     exclusively grabbed and report key-down/key-up, so long-press is just a
     timing check on release — no new input surface needed, hands stay off
     the screen. Recommended default.
  2. Corner-tap zone (e.g. top-left) = ascend. Fallback if long-press timing
     feels bad against page-turn taps in testing.
- **Depth stack**: `[]Level{ kind, siblingIndex, scrollOffset }`, push on tap,
  pop on ascend, mutate `siblingIndex`/`scrollOffset` in place on
  forward/back. Critically: **re-entering a level restores its stored
  offset rather than rebuilding it** — this is what makes lateral moves feel
  free of full-page redraw cost on e-ink. Each `Level` should be cheap to
  regenerate deterministically from `(kind, data, offset)` rather than
  needing to diff against a previous render.
- **Edge behavior**: clamp at the first/last sibling (no-op past the edge).
  Auto-ascend-at-edge was considered and rejected for now — it collapses
  forward/back into one gesture but risks an accidental level change while
  paging quickly. Revisit after living with clamping.

Current depths, top to bottom:

```
Level 0 (categories):  Summary | Article Index | Ponderings Deck | …
Level 1:               index-list pages | pondering-deck cards
Level 2:               article pages | pondering notebook pages
```

**Implication for the existing app**: `article.go` currently turns pages via
tap-on-edge. Under this model, *all* paging — at every depth — goes through
the physical buttons; tap is reserved exclusively for descending a level.
That's a net simplification, but it means removing the edge-tap page-turn
behavior rather than layering the new model on top of it.

Build order: wire the depth stack against fixture/mock content for all three
category types (reuse the existing `-tags sim` PNG-snapshot workflow — same
pattern `app/` already uses for highlight testing) before any real content
lands. This is pure navigation-shell work; get it feeling right with fake
data first.

---

## 1. Ponderings

A small deck of live threads, each with a running timeline of touches, not a
static note.

**Data model** (extends `store.go`'s pattern):
```
Pondering{
  id, title, created_at, last_touched_at
  structured_notes   // GPT-4o's rolling summary, server-produced
  pages: []Page{ ts, stroke_series | image_ref, ocr_gloss }
}
```

**Card (deck-level, full screen)**: title + rough bullets (the structured
summary) + a small footer (created date, last-touched date, maybe a tick
mark per touch — cheap dwell signal, same honesty argument FIELD-NOTES.md
already makes about paragraph dwell).

**Tap → notebook (child level)**: page 0 = the structured summary, pages
1..N = chronological scribble pages, paginated with the physical buttons.
Scribble capture reuses the pen-note stroke capture already built for
highlights — no new input-layer work, just a new destination for the
strokes.

**Creation**: explicit only, for now — "push this as a Pondering" from
chat, or "save this end-of-article gloss as a Pondering" (see §3). No
ambient/implicit capture; keep the deck intentional rather than another
inbox.

**Decay**: five Ponderings is the target mental model. Without some decay
mechanic this becomes fifty within a couple months. Cheapest version: deck
order is last-touched-first, and a stale Pondering (no touch in N weeks)
gets flagged for folding into a periodic digest rather than deleted outright.
Don't build this in v1 — but leave `last_touched_at` in the schema now so it
exists when you do.

**Where summarization happens**: server-side only. The device captures and
displays; it has no model. GPT-4o runs against the synced stroke/image data
during sync, same shape as the planned handwriting-to-text pipeline in
`FIELD-NOTES.md` §7.

Build order: (1) deck + notebook navigation against fixtures, (2) local
Pondering store + queue entries reusing `store.go`'s append-only pattern,
(3) scribble capture (reuse existing pen-note code), (4) server-side
summarization wired into sync.

---

## 2. Margin-boosting gesture

Hold the button, drag the pen sideways, and the paragraph under the pen
gets a wider margin, reflowing just that paragraph.

**Gesture detection**: on button-down, enter margin-adjust mode targeting
whichever paragraph the pen is hovering nearest — pen hover tracking
(`ABS_MT_DISTANCE` while `pressure=0`) already works per the riddle
experiment's stylus findings, so "which paragraph" doesn't need a contact
tap, just proximity.

**Live feedback vs. commit**: e-ink updates cost ~100ms each
(`FIELD-NOTES.md` §2) and reflow means recomputing that paragraph's line
breaks — too expensive to do continuously during a drag. Show a cheap ghost
indicator (a line or shaded band marking the candidate margin width) during
the drag; only run the actual paragraph reflow once the button/drag ends.
This is the same "flush ≤16Hz, one settling pass on gesture end" discipline
already used for pen ink.

**Layout work**: per-paragraph reflow (not whole-page) is the biggest lift
here — it's a text-layout problem, not an input problem. Worth prototyping
the reflow math in the simulator with no device involved before touching
the gesture/input side at all.

**Data model**: `margin_width` per paragraph per article (small int, steps
toward 50/50), synced alongside highlights. Note this is itself a free
attention signal — a widened paragraph with no note still says "this got
noticed," worth feeding into which paragraphs `§3`'s gloss pass considers.

**Soft lock, not hard lock**: once a paragraph has both a widened margin and
an annotation, default to not offering further widening — but make it a
default, not an irreversible rule. A permanently-cramped paragraph with no
way back is worse than an occasional accidental extra widen.

**Pen vs. finger inside the widened margin**: `ABS_MT_TOOL_TYPE` genuinely
distinguishes pen (1) from finger (0) on this digitizer (confirmed in the
riddle stylus findings), so this half is safe to build as designed — pen
contact writes ink, finger contact scrolls the margin's own note history.
That means the widened margin needs to be its own small scrollable
sub-canvas with an independent content offset, not just extra page width.

Build order: (1) paragraph-level layout metadata + reflow function
(prototype in sim), (2) hold+drag gesture detection, (3) ghost preview
during drag, (4) commit + reflow on release, (5) pen/finger split scrolling
inside the widened margin.

---

## 3. Paragraph-level annotation → gloss card

At article-close, crop each annotated paragraph (highlight, underline,
note, or now widened margin) as rendered — ink and all — and send it to
GPT-4o vision with surrounding text as context, to get back "here's what
you were thinking" per paragraph.

**Paragraph bounding boxes** already exist implicitly — the layout engine
that paginates `article.go` knows where each paragraph sits; the crop
utility is a thin wrapper over that, not new layout work.

**Batch at close, not live**: sending a vision call per scribble is both
laggy and unnecessary. Batch the whole article's annotated paragraphs into
one sync-time job.

**Surfacing**: a single end-of-article card (an extra page appended after
the article, or a "Thoughts" child level under it) listing
`{paragraph_ref, gloss}` pairs — not a per-paragraph interrupt. Simpler, and
matches the natural "I'm closing this article" moment rather than
fragmenting the read.

**Wire directly into Ponderings**: this end-of-article card is a natural
Pondering candidate. Give it a one-tap "→ Pondering" action rather than
treating capture-from-chat and capture-from-article as two separate
pipelines feeding two card types — they should share the same
`Pondering{}` target.

Build order: (1) paragraph crop utility over existing layout rects, (2)
server endpoint: batch of crops + context → GPT-4o vision → glosses, (3)
render the end-of-article card, (4) wire the "→ Pondering" action.

---

## Cross-cutting notes

- §1 and §3 want the same ingestion shape: image/strokes + context →
  GPT-4o → structured gloss, computed server-side during sync. Build that
  as one shared path, not two.
- §2's margin-width data is an input signal to §3's paragraph selection,
  not a separate feature.
- Everything here assumes the device stays "room-temperature" — captures
  locally, never blocks on network, syncs deltas at wake. See
  `ARCHITECTURE.md` for how that boundary is drawn against the server.
