package main

import (
	"os"
	"strings"
	"testing"
)

func tmpStore(t *testing.T) *MemoryStore {
	t.Helper()
	return &MemoryStore{dir: t.TempDir()}
}

func TestRoundTripAndReload(t *testing.T) {
	s := tmpStore(t)
	strokes := Strokes{{{10, 20, 3}, {14, 24, 3}, {100, 120, 2}}}
	s.Append(1751856000, "hello\ttom\nnewline", "Hello. Who writes?", strokes)

	s2 := &MemoryStore{dir: s.dir}
	s2.load()
	if len(s2.Entries) != 1 {
		t.Fatalf("entries=%d", len(s2.Entries))
	}
	if s2.Entries[0].Transcript != "hello\ttom\nnewline" {
		t.Fatalf("transcript=%q", s2.Entries[0].Transcript)
	}
	if s2.Entries[0].Reply != "Hello. Who writes?" {
		t.Fatalf("reply=%q", s2.Entries[0].Reply)
	}
	back := s2.LoadStrokes(1751856000)
	if len(back) != 1 {
		t.Fatalf("strokes=%d", len(back))
	}
	if back[0][0] != (InkPt{10, 20, 3}) || back[0][len(back[0])-1] != (InkPt{100, 120, 2}) {
		t.Fatalf("endpoints wrong: %v", back[0])
	}
}

func TestDecimationKeepsEndpointsDropsDense(t *testing.T) {
	var dense []InkPt
	for i := 0; i < 100; i++ {
		dense = append(dense, InkPt{i, 0, 3})
	}
	thin := decimate(Strokes{dense})
	if len(thin[0]) >= 40 {
		t.Fatalf("kept too many: %d", len(thin[0]))
	}
	if thin[0][0] != (InkPt{0, 0, 3}) || thin[0][len(thin[0])-1] != (InkPt{99, 0, 3}) {
		t.Fatalf("endpoints wrong: %v", thin[0])
	}
}

func TestPruneForgetsOldest(t *testing.T) {
	s := tmpStore(t)
	for i := uint64(1); i <= maxMemories+5; i++ {
		s.Append(i, "t", "r", Strokes{{{1, 1, 1}}})
	}
	if len(s.Entries) != maxMemories {
		t.Fatalf("entries=%d", len(s.Entries))
	}
	if s.Entries[0].ID != 6 {
		t.Fatalf("oldest=%d", s.Entries[0].ID)
	}
	if _, err := os.Stat(s.strokesPath(1)); err == nil {
		t.Fatal("pruned strokes file still exists")
	}
	if _, err := os.Stat(s.strokesPath(6)); err != nil {
		t.Fatal("kept strokes file missing")
	}
}

func TestCatalogIsNumberedNewestFirst(t *testing.T) {
	s := tmpStore(t)
	s.Append(1751856000, "about the garden", "…", Strokes{{{1, 1, 1}}})
	s.Append(1751942400, "about the rain", "…", Strokes{{{1, 1, 1}}})
	lines, ids := s.Catalog(10)
	if ids[0] != 1751942400 || ids[1] != 1751856000 {
		t.Fatalf("ids=%v", ids)
	}
	if !strings.HasPrefix(lines[0], "1. ") || !strings.Contains(lines[0], "about the rain") {
		t.Fatalf("line0=%q", lines[0])
	}
	if !strings.Contains(lines[1], "about the garden") {
		t.Fatalf("line1=%q", lines[1])
	}
}

func TestSpokenDatesReadLikeADiary(t *testing.T) {
	// 2026-07-06 23:30 UTC.
	s := spokenDate(1783467000, 0)
	if !strings.Contains(s, "of July") {
		t.Fatalf("%q", s)
	}
	if !strings.Contains(s, "6th") && !strings.Contains(s, "7th") {
		t.Fatalf("%q", s)
	}
}
