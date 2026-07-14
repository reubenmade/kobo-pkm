package main

import (
	"fmt"
	"image"
	"log"
	"os"
	"strings"
	"time"
)

// The diary itself: riddle's main.rs state machine, ported. Write on the
// page; after a pause the diary drinks your ink, and an answer writes itself
// onto the page in a flowing hand, then fades.
//
// The Diary is driven from outside: HandleTouch feeds it mapped digitizer
// events, Step advances animations and the oracle turn. Both mains (device
// and simulator) share it; the simulator drives Step with a fake clock.

const (
	idleCommit     = 2800 * time.Millisecond
	oraclePatience = 120 * time.Second // thinking models can lead with silence
	replyPx        = 96.0
	marginX        = 100
	// E-ink updates cost ~100ms wall each on the hwtcon driver — coalesce
	// dirty regions and flush at most this often (FIELD-NOTES: ≤16Hz).
	flushEvery = 60 * time.Millisecond
)

type dstate int

const (
	stListening dstate = iota
	stDrinking
	stThinking
	stReplying
	stLingering
	stFadingReply
	stHelp
	stConjuring
	stMemoryShown
)

// WritePlan is a reply being written onto the page: pre-positioned strokes.
type WritePlan struct {
	strokes [][]Pt
	strokeI int
	pointI  int
	region  BBox
	nextY   int // where the next streamed chunk's first line starts
}

// ConjurePlan is a memory being rewritten onto the page: strokes with their
// original radii, drawn in faded ink.
type ConjurePlan struct {
	strokes Strokes
	strokeI int
	pointI  int
	region  BBox
}

type Diary struct {
	disp    Display
	cfg     Config
	W, H    int
	oracle  Oracle
	store   *MemoryStore
	pngPath string
	now     func() time.Time

	ink     *Ink
	state   dstate
	penDown bool
	// Raw contact, tracked in every state (the guide dismisses on it).
	// stylusOn is the level; stylusTapped latches any contact seen since the
	// last Step, so a tap that starts AND ends between Steps still registers.
	stylusOn     bool
	stylusTapped bool

	// Coalesced e-ink flushing: pen-waveform and quality dirty regions.
	penDirty  BBox
	autoDirty BBox
	lastFlush time.Time

	// State fields (validity depends on state).
	lastPen time.Time // Listening; zero = no ink yet
	stage   uint32    // Drinking / FadingReply
	next    time.Time // animation pacing
	region  BBox
	rx      <-chan Event // oracle turn stream; nil when none
	pulse   time.Time    // Thinking blot
	blotOn  bool
	since   time.Time // Thinking start
	plan    *WritePlan
	cplan   *ConjurePlan
	saved   []byte // Conjuring/MemoryShown: today's page. nil = dismissed
	until   time.Time
	panel   *Help // Help; nil = dismissed, waiting pen-up

	// The turn being remembered: strokes captured at commit, transcript and
	// reply accumulated as they stream, stored when the turn completes.
	turnID         uint64
	turnStrokes    Strokes
	turnReply      strings.Builder
	turnTranscript string
	turnFailed     bool

	Quit bool
}

func NewDiary(disp Display, cfg Config, oracle Oracle, store *MemoryStore, pngPath string) *Diary {
	b := disp.Bounds()
	d := &Diary{
		disp:    disp,
		cfg:     cfg,
		W:       b.Dx(),
		H:       b.Dy(),
		oracle:  oracle,
		store:   store,
		pngPath: pngPath,
		now:     time.Now,
		ink:     NewInk(),
		state:   stListening,
	}
	d.penDirty = EmptyBBox()
	d.autoDirty = EmptyBBox()
	// Blank page.
	FillRect(d.canvas(), b, WHITE)
	disp.Refresh(b, RefreshFull)
	return d
}

func (d *Diary) canvas() *image.RGBA { return d.disp.Canvas() }

func (d *Diary) bounds() image.Rectangle { return d.disp.Bounds() }

// update queues a region for the next coalesced e-ink flush. Pen-waveform
// (A2, pure black/white, instant) for ink and animations; quality (AUTO)
// for panels and faded ink.
func (d *Diary) update(b BBox, pen bool) {
	if b.IsEmpty() {
		return
	}
	if pen {
		d.penDirty.Union(b)
	} else {
		d.autoDirty.Union(b)
	}
}

func (d *Diary) fullRefresh() {
	d.penDirty = EmptyBBox()
	d.autoDirty = EmptyBBox()
	d.disp.Refresh(d.bounds(), RefreshFull)
	d.lastFlush = d.now()
}

func (d *Diary) flushIfDue() {
	if d.now().Sub(d.lastFlush) < flushEvery {
		return
	}
	if !d.penDirty.IsEmpty() {
		d.disp.Refresh(d.penDirty.Rect(d.bounds()), RefreshPen)
		d.penDirty = EmptyBBox()
		d.lastFlush = d.now()
	}
	if !d.autoDirty.IsEmpty() {
		d.disp.Refresh(d.autoDirty.Rect(d.bounds()), RefreshAuto)
		d.autoDirty = EmptyBBox()
		d.lastFlush = d.now()
	}
}

// penRadius maps digitizer pressure (0..4096, 0 = unknown) to a brush radius.
func penRadius(pressure int) int {
	if pressure <= 0 {
		return 3
	}
	return 2 + pressure*3/4096
}

// HandleTouch feeds one mapped digitizer event into the diary.
func (d *Diary) HandleTouch(t Touch) {
	if t.Kind == TouchHover {
		// The pen gliding above the page is not contact: it must not ink,
		// dismiss, or split strokes. (Future interaction hook, though.)
		return
	}
	writing := t.Kind == TouchDown || t.Kind == TouchMove
	d.stylusOn = writing
	if writing {
		d.stylusTapped = true
	}
	if !writing {
		if d.penDown {
			d.penDown = false
			d.ink.PenUp()
			if d.state == stListening {
				d.lastPen = d.now()
			}
		}
		return
	}
	switch d.state {
	case stListening:
		d.penDown = true
		var dirty BBox
		if t.Eraser {
			dirty = d.ink.ErasePoint(d.canvas(), t.X, t.Y, 22)
		} else {
			r := penRadius(t.Pressure)
			if t.Button {
				r *= 2 // the side button writes with a broad nib
			}
			dirty = d.ink.PenPoint(d.canvas(), t.X, t.Y, r)
		}
		d.update(dirty, true)
		d.lastPen = d.now()
	case stLingering:
		d.state = stFadingReply
		d.stage = 0
		d.next = d.now()
	}
}

// tryRecv polls the oracle stream. got=false, closed=false means no event yet.
func tryRecv(rx <-chan Event) (ev Event, got, closed bool) {
	if rx == nil {
		return Event{}, false, true
	}
	select {
	case ev, ok := <-rx:
		if !ok {
			return Event{}, false, true
		}
		return ev, true, false
	default:
		return Event{}, false, false
	}
}

// buildCtx is what the diary sends alongside the page: its memory of recent
// turns and the catalog the oracle picks conjured pages from.
func (d *Diary) buildCtx() *TurnContext {
	if d.store == nil {
		return &TurnContext{}
	}
	lines, ids := d.store.Catalog(40)
	return &TurnContext{
		History:      d.store.RecentDialogue(d.cfg.MemoryTurns),
		CatalogLines: lines,
		CatalogIDs:   ids,
	}
}

// Step advances the diary: coalesced ink flush, then the state machine.
// Call it every few milliseconds.
func (d *Diary) Step() {
	now := d.now()
	d.flushIfDue()

	switch d.state {
	case stListening:
		if !d.lastPen.IsZero() && !d.penDown && now.Sub(d.lastPen) >= idleCommit && !d.ink.IsEmpty() {
			d.commitPage(now)
		}

	case stDrinking:
		const stages = 14
		if !now.Before(d.next) {
			DissolvePass(d.canvas(), d.region, d.stage, stages)
			d.update(d.region, true)
			if d.stage+1 >= stages {
				d.ink.Clear()
				d.state = stThinking
				d.pulse = now
				d.blotOn = false
				d.since = now
			} else {
				d.stage++
				d.next = now.Add(70 * time.Millisecond)
			}
		}

	case stThinking:
		d.stepThinking(now)

	case stReplying:
		d.stepReplying(now)

	case stLingering:
		if !now.Before(d.until) {
			d.state = stFadingReply
			d.stage = 0
			d.next = now
		}

	case stFadingReply:
		const stages = 10
		if !now.Before(d.next) {
			DissolvePass(d.canvas(), d.region, d.stage, stages)
			d.update(d.region, true)
			if d.stage+1 >= stages {
				d.fullRefresh()
				d.toListening()
			} else {
				d.stage++
				d.next = now.Add(80 * time.Millisecond)
			}
		}

	case stHelp:
		if d.panel != nil {
			if d.stylusTapped || !now.Before(d.until) {
				region := d.panel.Dismiss(d.canvas())
				d.update(region, false)
				log.Printf("guide dismissed")
				d.panel = nil
			}
		} else if !d.stylusOn {
			// Dismissed: the closing touch was swallowed; listen again.
			d.toListening()
		}

	case stConjuring:
		d.stepConjuring(now)

	case stMemoryShown:
		if d.saved != nil {
			if d.stylusTapped || !now.Before(d.until) {
				// The paper swallows its memory; today's page returns.
				PasteRect(d.canvas(), d.bounds(), d.saved)
				d.saved = nil
				d.fullRefresh()
				log.Printf("memory dismissed")
			}
		} else if !d.stylusOn {
			d.toListening()
		}
	}

	d.stylusTapped = false
}

func (d *Diary) toListening() {
	d.state = stListening
	d.lastPen = time.Time{}
	d.plan = nil
	d.cplan = nil
	d.saved = nil
	d.panel = nil
	d.rx = nil
}

// commitPage: the pen has rested. Absorb a "?", or rasterize the page and
// ask the oracle while the ink is drunk.
func (d *Diary) commitPage(now time.Time) {
	if d.regionAllWhite(d.ink.BBox) {
		// Everything was erased before the pause: nothing to commit.
		d.ink.Clear()
		d.lastPen = time.Time{}
		return
	}
	if LooksLikeQuestionMark(d.ink.StrokeList()) {
		// Absorb the "?" and open the guide instead of asking.
		q := d.ink.BBox.Rect(d.bounds())
		FillRect(d.canvas(), q, WHITE)
		d.disp.Refresh(q, RefreshAuto)
		d.ink.Clear()
		d.panel = ShowHelp(d.canvas())
		d.disp.Refresh(d.panel.Region.Rect(d.bounds()), RefreshAuto)
		log.Printf("guide shown")
		d.state = stHelp
		d.until = now.Add(45 * time.Second)
		return
	}
	if d.oracle == nil {
		// No spirit at all: don't eat ink that nothing will answer — leave
		// the writing and put the reason below.
		y := min(d.ink.BBox.Y1+90, d.H-400)
		d.plan = d.planReply(oracleExcuse("no oracle"), y)
		d.rx = nil
		d.state = stReplying
		d.next = now
		return
	}
	if err := d.ink.ToPNG(d.canvas(), d.pngPath); err != nil {
		log.Printf("rasterize failed: %v", err)
	}
	// Remember this page: strokes now (they're cleared after the drink),
	// transcript/reply as they stream.
	d.turnID = uint64(now.Unix())
	d.turnStrokes = append(Strokes(nil), d.ink.StrokeList()...)
	d.turnReply.Reset()
	d.turnTranscript = ""
	d.turnFailed = false
	// Ask NOW: the model streams while the diary drinks the ink, hiding most
	// of the reply latency in the animation.
	d.rx = d.oracle.Ask(d.pngPath, d.buildCtx())
	if !d.cfg.KeepPage {
		os.Remove(d.pngPath)
	}
	d.region = d.ink.BBox
	d.state = stDrinking
	d.stage = 0
	d.next = now
}

func (d *Diary) clearBlot() {
	cx, cy := d.W/2, d.H/2
	FillRect(d.canvas(), image.Rect(cx-14, cy-14, cx+14, cy+14), WHITE)
	b := EmptyBBox()
	b.Add(cx, cy, 14)
	d.update(b, true)
}

func (d *Diary) stepThinking(now time.Time) {
	ev, got, closed := tryRecv(d.rx)
	switch {
	case got:
		d.clearBlot()
		switch ev.Kind {
		case EvShow:
			// An incantation: the rest of this turn is the conjured memory.
			d.rx = nil
			if !d.conjure(ev.ID) {
				log.Printf("memory %d is missing", ev.ID)
				d.turnFailed = true
				d.plan = d.planReply(oracleExcuse("lost page"), -1)
				d.state = stReplying
				d.next = now
			}
		case EvInk:
			d.turnReply.WriteString(ev.Text)
			d.plan = d.planReply(ev.Text, -1)
			d.state = stReplying
			d.next = now
		case EvTranscript:
			// Transcript with no prose (model skipped the reply): remember
			// the words, keep waiting.
			d.turnTranscript = ev.Text
		case EvErr:
			log.Printf("oracle failed: %s", ev.Text)
			d.turnFailed = true
			d.plan = d.planReply(oracleExcuse(ev.Text), -1)
			d.rx = nil
			d.state = stReplying
			d.next = now
		}
	case closed:
		d.toListening()
	default:
		if now.Sub(d.since) >= oraclePatience {
			// The oracle never answered: stop pulsing and say so.
			log.Printf("oracle timed out after %v", oraclePatience)
			d.clearBlot()
			d.turnFailed = true
			d.plan = d.planReply(oracleExcuse("timed out"), -1)
			d.rx = nil
			d.state = stReplying
			d.next = now
		} else if now.Sub(d.pulse) >= 600*time.Millisecond {
			cx, cy := d.W/2, d.H/2
			if d.blotOn {
				FillRect(d.canvas(), image.Rect(cx-14, cy-14, cx+14, cy+14), WHITE)
			} else {
				Stamp(d.canvas(), cx, cy, 9, BLACK)
			}
			b := EmptyBBox()
			b.Add(cx, cy, 14)
			d.update(b, true)
			d.pulse = now
			d.blotOn = !d.blotOn
		}
	}
}

func (d *Diary) stepReplying(now time.Time) {
	// More of the reply may still be streaming in: append each new chunk
	// below what is already planned, mid-animation.
	if d.rx != nil {
		ev, got, closed := tryRecv(d.rx)
		switch {
		case got:
			switch ev.Kind {
			case EvInk:
				if d.plan.nextY > d.H-200 {
					// The page is full: let the rest go unwritten rather
					// than inking below the visible page.
					log.Printf("reply reached the page bottom; trailing text dropped")
					d.rx = nil
				} else {
					d.turnReply.WriteString(" ")
					d.turnReply.WriteString(ev.Text)
					d.appendReply(ev.Text)
				}
			case EvTranscript:
				d.turnTranscript = ev.Text // the close is still coming
			case EvShow:
				log.Printf("conjuring directive mid-reply ignored")
			case EvErr:
				log.Printf("oracle failed mid-reply: %s", ev.Text)
				d.turnFailed = true
				d.rx = nil
			}
		case closed:
			d.rx = nil
		}
	}
	if now.Before(d.next) {
		return
	}
	dirty := EmptyBBox()
	budget := 26
	for budget > 0 && d.plan.strokeI < len(d.plan.strokes) {
		stroke := d.plan.strokes[d.plan.strokeI]
		if d.plan.pointI >= len(stroke) {
			d.plan.strokeI++
			d.plan.pointI = 0
			continue
		}
		p := stroke[d.plan.pointI]
		if d.plan.pointI > 0 {
			q := stroke[d.plan.pointI-1]
			BrushLine(d.canvas(), q.X, q.Y, p.X, p.Y, 2, BLACK)
		} else {
			Stamp(d.canvas(), p.X, p.Y, 2, BLACK)
		}
		dirty.Add(p.X, p.Y, 4)
		d.plan.pointI++
		budget--
	}
	d.update(dirty, true)
	if d.plan.strokeI >= len(d.plan.strokes) && d.rx == nil {
		// The turn is complete: the diary remembers it.
		if !d.turnFailed && d.turnReply.Len() > 0 && d.store != nil {
			d.store.Append(d.turnID, d.turnTranscript, strings.TrimSpace(d.turnReply.String()), d.turnStrokes)
		}
		d.turnStrokes = nil
		chars := 0
		for _, s := range d.plan.strokes {
			chars += len(s)
		}
		linger := time.Duration(4000+chars*2) * time.Millisecond
		if linger > 20*time.Second {
			linger = 20 * time.Second
		}
		d.region = d.plan.region
		d.state = stLingering
		d.until = now.Add(linger)
	} else {
		d.next = now.Add(14 * time.Millisecond)
	}
}

func (d *Diary) stepConjuring(now time.Time) {
	if d.stylusTapped {
		// The writer interrupts: today's page returns at once.
		PasteRect(d.canvas(), d.bounds(), d.saved)
		d.fullRefresh()
		d.saved = nil
		d.state = stMemoryShown
		d.until = now
		return
	}
	if now.Before(d.next) {
		return
	}
	// The memory pours back faster than Tom writes: it is remembered, not
	// composed.
	dirty := EmptyBBox()
	budget := 48
	for budget > 0 && d.cplan.strokeI < len(d.cplan.strokes) {
		stroke := d.cplan.strokes[d.cplan.strokeI]
		if d.cplan.pointI >= len(stroke) {
			d.cplan.strokeI++
			d.cplan.pointI = 0
			continue
		}
		p := stroke[d.cplan.pointI]
		if d.cplan.pointI > 0 {
			q := stroke[d.cplan.pointI-1]
			BrushLine(d.canvas(), q.X, q.Y, p.X, p.Y, min(p.R, q.R+1), FADED)
		} else {
			Stamp(d.canvas(), p.X, p.Y, p.R, FADED)
		}
		dirty.Add(p.X, p.Y, p.R+2)
		d.cplan.pointI++
		budget--
	}
	d.update(dirty, false) // faded ink needs a grayscale waveform
	if d.cplan.strokeI >= len(d.cplan.strokes) {
		d.region = d.cplan.region
		d.state = stMemoryShown
		d.until = now.Add(120 * time.Second)
	} else {
		d.next = now.Add(10 * time.Millisecond)
	}
}

// conjure summons a remembered page: snapshot today's page, clear the paper,
// and plan the memory's rewriting — the date in a small hand, the writer's
// own strokes exactly as they were penned, Tom's old reply beneath — all in
// faded ink. Returns false if the memory is gone.
func (d *Diary) conjure(id uint64) bool {
	if d.store == nil {
		return false
	}
	entry := d.store.Get(id)
	if entry == nil {
		return false
	}
	strokes := d.store.LoadStrokes(id)
	log.Printf("conjuring memory %d (%s)", id, d.store.SpokenDate(id))

	d.saved = CopyRect(d.canvas(), d.bounds())
	FillRect(d.canvas(), d.bounds(), WHITE)
	d.disp.Refresh(d.bounds(), RefreshAuto)

	var all Strokes
	region := EmptyBBox()
	inkBottom := 64

	// The date, small and centered near the top, like a diary heading.
	date := d.store.SpokenDate(entry.ID)
	raster := RasterizeLine(date, 54.0)
	raster.Thin()
	x0 := (d.W - raster.W) / 2
	for _, stroke := range raster.Trace() {
		mapped := make([]InkPt, len(stroke))
		for i, p := range stroke {
			mapped[i] = InkPt{x0 + p.X, 64 + p.Y, 1}
			region.Add(mapped[i].X, mapped[i].Y, 3)
			inkBottom = max(inkBottom, mapped[i].Y)
		}
		all = append(all, mapped)
	}

	// The writer's own hand, exactly as it was penned.
	for _, stroke := range strokes {
		for _, p := range stroke {
			region.Add(p.X, p.Y, p.R+2)
			inkBottom = max(inkBottom, p.Y)
		}
		all = append(all, stroke)
	}

	// Tom's old reply, below.
	if entry.Reply != "" {
		y := min(inkBottom+130, d.H-400)
		reply := d.planReply(entry.Reply, y)
		for _, stroke := range reply.strokes {
			mapped := make([]InkPt, len(stroke))
			for i, p := range stroke {
				mapped[i] = InkPt{p.X, p.Y, 2}
				region.Add(p.X, p.Y, 4)
			}
			all = append(all, mapped)
		}
	}

	d.cplan = &ConjurePlan{strokes: all, region: region}
	d.state = stConjuring
	d.next = d.now()
	return true
}

// planReply lays out reply text and produces screen-space strokes. yStart
// continues a streamed reply below its previous chunk; -1 places the first.
func (d *Diary) planReply(text string, yStart int) *WritePlan {
	maxW := d.W - 2*marginX
	lines := Wrap(text, replyPx, maxW)
	lineH := int(replyPx * 1.25)
	totalH := lineH * len(lines)
	y := yStart
	if y < 0 {
		y = max((d.H-totalH)/3, 60)
	}
	plan := &WritePlan{region: EmptyBBox()}
	seed := uint32(0x1234)
	jitter := func() int {
		seed = seed*1664525 + 1013904223
		return int((seed>>16)%7) - 3
	}

	for _, lineText := range lines {
		raster := RasterizeLine(lineText, replyPx)
		raster.Thin()
		lineStrokes := raster.Trace()
		x0 := (d.W - raster.W) / 2
		wobble := jitter()
		for _, s := range lineStrokes {
			mapped := make([]Pt, len(s))
			for i, p := range s {
				mapped[i] = Pt{x0 + p.X, y + p.Y + wobble}
				plan.region.Add(mapped[i].X, mapped[i].Y, 5)
			}
			plan.strokes = append(plan.strokes, mapped)
		}
		y += lineH
	}
	plan.nextY = y
	return plan
}

// appendReply splices a streamed continuation chunk into a running write
// animation.
func (d *Diary) appendReply(more string) {
	cont := d.planReply(more, d.plan.nextY)
	if len(cont.strokes) == 0 {
		return
	}
	d.plan.region.Union(cont.region)
	d.plan.strokes = append(d.plan.strokes, cont.strokes...)
	d.plan.nextY = cont.nextY
}

// regionAllWhite: true if the region no longer holds dark pixels (erased).
func (d *Diary) regionAllWhite(region BBox) bool {
	if region.IsEmpty() {
		return true
	}
	for y := region.Y0; y <= region.Y1; y++ {
		for x := region.X0; x <= region.X1; x++ {
			if Luma(d.canvas(), x, y) < 200 {
				return false
			}
		}
	}
	return true
}

// oracleExcuse: what Tom writes when the spirit cannot answer — short, in a
// diary's voice, but specific enough to act on.
func oracleExcuse(e string) string {
	switch {
	case strings.Contains(e, "no oracle"):
		return "The diary lies dormant: it found no oracle. Put an oracle_key in config.txt, then open me again."
	case strings.HasPrefix(e, "http 401"), strings.HasPrefix(e, "http 403"):
		return "The oracle refused the diary's key. Check oracle_key in config.txt."
	case strings.HasPrefix(e, "http "):
		code := e
		if i := strings.IndexByte(e, ':'); i > 0 {
			code = e[:i]
		}
		return fmt.Sprintf("The oracle rejected the diary's plea (%s). Check the model and endpoint in config.txt.", code)
	case strings.Contains(e, "request failed"), strings.Contains(e, "timed out"):
		return "The diary cannot reach its oracle. Is the Kobo connected to Wi-Fi?"
	case strings.Contains(e, "empty reply"):
		return "The spirit read your words but said nothing. Write again."
	default:
		return "The ink blurred before it could answer. Write again."
	}
}
