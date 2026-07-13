package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Note attached to a highlight. Pen notes keep raw strokes so the server
// can run handwriting-to-text on them later.
type Note struct {
	Type    string     `json:"type"` // "text" | "pen"
	Text    string     `json:"text,omitempty"`
	Strokes [][][2]int `json:"strokes,omitempty"` // normalized to the pen pad
}

type Highlight struct {
	ID        string `json:"id"`
	Article   string `json:"article"`
	Start     int    `json:"start"` // word index range within the article
	End       int    `json:"end"`
	Text      string `json:"text"`
	Color     string `json:"color,omitempty"` // hlPalette name
	Note      *Note  `json:"note,omitempty"`
	CreatedAt string `json:"created_at"`
}

// Store persists highlights to disk and mirrors every mutation into a queue
// directory. A future sync worker drains queue/ to the listen-later server;
// the app never blocks on the network.
type Store struct {
	dir        string
	Highlights []Highlight
}

func OpenStore(dir string) *Store {
	s := &Store{dir: dir}
	os.MkdirAll(filepath.Join(dir, "queue"), 0755)
	data, err := os.ReadFile(s.path())
	if err == nil {
		if err := json.Unmarshal(data, &s.Highlights); err != nil {
			log.Printf("store: corrupt highlights.json, starting fresh: %v", err)
		}
	}
	return s
}

func (s *Store) path() string { return filepath.Join(s.dir, "highlights.json") }

func (s *Store) flush() {
	data, _ := json.MarshalIndent(s.Highlights, "", "  ")
	if err := os.WriteFile(s.path(), data, 0644); err != nil {
		log.Printf("store: write: %v", err)
	}
}

func (s *Store) enqueue(op string, h Highlight) {
	item := struct {
		Op        string    `json:"op"`
		Highlight Highlight `json:"highlight"`
	}{op, h}
	data, _ := json.MarshalIndent(item, "", "  ")
	name := fmt.Sprintf("%d-%s-%s.json", time.Now().UnixMilli(), op, h.ID)
	if err := os.WriteFile(filepath.Join(s.dir, "queue", name), data, 0644); err != nil {
		log.Printf("store: queue write: %v", err)
	}
}

func (s *Store) Add(h Highlight) {
	h.ID = fmt.Sprintf("hl-%d", time.Now().UnixNano())
	h.CreatedAt = time.Now().Format(time.RFC3339)
	s.Highlights = append(s.Highlights, h)
	s.flush()
	s.enqueue("add", h)
	log.Printf("store: added %s (%q, note=%v)", h.ID, h.Text, h.Note != nil)
}

// Update replaces an existing highlight (note edits) and queues the change.
func (s *Store) Update(h Highlight) {
	for i := range s.Highlights {
		if s.Highlights[i].ID == h.ID {
			s.Highlights[i] = h
			s.flush()
			s.enqueue("update", h)
			log.Printf("store: updated %s (note=%v)", h.ID, h.Note != nil)
			return
		}
	}
}

func (s *Store) Delete(id string) {
	for i, h := range s.Highlights {
		if h.ID == id {
			s.Highlights = append(s.Highlights[:i], s.Highlights[i+1:]...)
			s.flush()
			s.enqueue("delete", h)
			return
		}
	}
}

func (s *Store) ForArticle(article string) []Highlight {
	var out []Highlight
	for _, h := range s.Highlights {
		if h.Article == article {
			out = append(out, h)
		}
	}
	return out
}
