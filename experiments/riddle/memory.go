package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The diary's memory. Every finished turn is kept — the writer's actual pen
// strokes, a transcription of their words, and Tom's reply — so a later
// incantation ("show me what I wrote about the garden") can conjure the page
// back in the writer's own hand. Port of riddle's memory.rs.
//
//   index.tsv      one line per memory: id \t transcript \t reply
//   <id>.strokes   the pen strokes: one line per stroke, "x,y,r;x,y,r;…"
//
// Delete the directory and the diary forgets; memory=off disables it.

// maxMemories is how many newest pages the diary keeps.
const maxMemories = 400

// minPointDist2: drop replay points closer than this (px²) to the last kept
// one. Handwriting stays faithful; files shrink several-fold.
const minPointDist2 = 9

type MemEntry struct {
	ID         uint64 // unix seconds when the page was committed
	Transcript string
	Reply      string
}

type MemoryStore struct {
	dir     string
	Entries []MemEntry
	tzHours float64
}

// OpenMemory opens (or starts) the diary's memory. Returns nil when off.
func OpenMemory(cfg Config, baseDir string) *MemoryStore {
	if cfg.MemoryOff {
		return nil
	}
	dir := cfg.MemoryDir
	if dir == "" {
		dir = filepath.Join(baseDir, "memories")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("memory disabled (%s: %v)", dir, err)
		return nil
	}
	s := &MemoryStore{dir: dir, tzHours: cfg.TZOffsetHours}
	s.load()
	return s
}

func (s *MemoryStore) indexPath() string { return filepath.Join(s.dir, "index.tsv") }
func (s *MemoryStore) strokesPath(id uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%d.strokes", id))
}

func (s *MemoryStore) load() {
	text, err := os.ReadFile(s.indexPath())
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(text), "\n") {
		cols := strings.SplitN(line, "\t", 3)
		if len(cols) != 3 {
			continue
		}
		id, err := strconv.ParseUint(cols[0], 10, 64)
		if err != nil {
			continue
		}
		s.Entries = append(s.Entries, MemEntry{ID: id, Transcript: unescapeTSV(cols[1]), Reply: unescapeTSV(cols[2])})
	}
}

// Append remembers a finished turn. Strokes are decimated before writing.
func (s *MemoryStore) Append(id uint64, transcript, reply string, strokes Strokes) {
	thin := decimate(strokes)
	var lines strings.Builder
	for _, st := range thin {
		for i, p := range st {
			if i > 0 {
				lines.WriteByte(';')
			}
			fmt.Fprintf(&lines, "%d,%d,%d", p.X, p.Y, p.R)
		}
		lines.WriteByte('\n')
	}
	if err := os.WriteFile(s.strokesPath(id), []byte(lines.String()), 0o644); err != nil {
		log.Printf("memory strokes not kept: %v", err)
	}
	entry := MemEntry{ID: id, Transcript: transcript, Reply: reply}
	line := fmt.Sprintf("%d\t%s\t%s\n", id, escapeTSV(transcript), escapeTSV(reply))
	f, err := os.OpenFile(s.indexPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		_, err = f.WriteString(line)
		f.Close()
	}
	if err != nil {
		log.Printf("memory not kept: %v", err)
		return
	}
	s.Entries = append(s.Entries, entry)
	s.prune()
}

// prune forgets the oldest pages beyond maxMemories.
func (s *MemoryStore) prune() {
	if len(s.Entries) <= maxMemories {
		return
	}
	dropN := len(s.Entries) - maxMemories
	for _, e := range s.Entries[:dropN] {
		os.Remove(s.strokesPath(e.ID))
	}
	s.Entries = append([]MemEntry(nil), s.Entries[dropN:]...)
	var out strings.Builder
	for _, e := range s.Entries {
		fmt.Fprintf(&out, "%d\t%s\t%s\n", e.ID, escapeTSV(e.Transcript), escapeTSV(e.Reply))
	}
	if err := os.WriteFile(s.indexPath(), []byte(out.String()), 0o644); err != nil {
		log.Printf("memory prune failed: %v", err)
	}
}

// LoadStrokes loads the pen strokes of one remembered page.
func (s *MemoryStore) LoadStrokes(id uint64) Strokes {
	text, err := os.ReadFile(s.strokesPath(id))
	if err != nil {
		return nil
	}
	var strokes Strokes
	for _, line := range strings.Split(string(text), "\n") {
		var stroke []InkPt
		for _, pt := range strings.Split(line, ";") {
			var x, y, r int
			if _, err := fmt.Sscanf(pt, "%d,%d,%d", &x, &y, &r); err == nil {
				stroke = append(stroke, InkPt{x, y, r})
			}
		}
		if len(stroke) > 0 {
			strokes = append(strokes, stroke)
		}
	}
	return strokes
}

func (s *MemoryStore) Get(id uint64) *MemEntry {
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			return &s.Entries[i]
		}
	}
	return nil
}

// RecentDialogue returns the last n turns as (transcript, reply) pairs,
// oldest first — the conversational memory riding along with each request.
func (s *MemoryStore) RecentDialogue(n int) [][2]string {
	var out [][2]string
	for i := len(s.Entries) - 1; i >= 0 && len(out) < n; i-- {
		e := s.Entries[i]
		if e.Transcript == "" {
			continue
		}
		out = append(out, [2]string{e.Transcript, e.Reply})
	}
	// reverse to oldest-first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Catalog is the numbered newest-first list shown to the oracle so it can
// pick a page to conjure. ids[i] belongs to catalog number i+1.
func (s *MemoryStore) Catalog(maxN int) ([]string, []uint64) {
	var lines []string
	var ids []uint64
	for i := len(s.Entries) - 1; i >= 0 && len(lines) < maxN; i-- {
		e := s.Entries[i]
		gist := oneLine(e.Transcript, 70)
		if strings.TrimSpace(e.Transcript) == "" {
			gist = "(reply: " + oneLine(e.Reply, 70) + ")"
		}
		lines = append(lines, fmt.Sprintf("%d. %s — %s", len(lines)+1, s.SpokenDate(e.ID), gist))
		ids = append(ids, e.ID)
	}
	return lines, ids
}

// oneLine collapses whitespace to single spaces and caps at max runes, so a
// multi-line transcript stays one catalog line.
func oneLine(s string, maxRunes int) string {
	joined := strings.Join(strings.Fields(s), " ")
	r := []rune(joined)
	if len(r) > maxRunes {
		r = r[:maxRunes]
	}
	return string(r)
}

func decimate(strokes Strokes) Strokes {
	var out Strokes
	for _, s := range strokes {
		var thin []InkPt
		for i, p := range s {
			keep := len(thin) == 0
			if !keep {
				last := thin[len(thin)-1]
				dx, dy := p.X-last.X, p.Y-last.Y
				keep = dx*dx+dy*dy >= minPointDist2 || i == len(s)-1
			}
			if keep {
				thin = append(thin, p)
			}
		}
		if len(thin) > 0 {
			out = append(out, thin)
		}
	}
	return out
}

func escapeTSV(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return strings.ReplaceAll(s, "\n", "\\n")
}

func unescapeTSV(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i == len(s)-1 {
			out.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 't':
			out.WriteByte('\t')
		case 'n':
			out.WriteByte('\n')
		default:
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

// SpokenDate: "the 6th of July, in the evening" — how the diary speaks of a
// moment. The Kobo clock runs on UTC; tz_offset (hours) nudges it local.
func (s *MemoryStore) SpokenDate(id uint64) string {
	return spokenDate(id, s.tzHours)
}

func spokenDate(id uint64, tzHours float64) string {
	t := time.Unix(int64(id)+int64(tzHours*3600), 0).UTC()
	d, h := t.Day(), t.Hour()
	suffix := "th"
	if d < 11 || d > 13 {
		switch d % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	tod := "late at night"
	switch {
	case h <= 4:
		tod = "in the small hours"
	case h <= 11:
		tod = "in the morning"
	case h <= 17:
		tod = "in the afternoon"
	case h <= 21:
		tod = "in the evening"
	}
	return fmt.Sprintf("the %d%s of %s, %s", d, suffix, t.Month().String(), tod)
}
