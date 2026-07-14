package main

import (
	"reflect"
	"testing"
)

// Ports of riddle's oracle.rs parser tests.

func drain(t *testing.T, events []Event) []Event {
	t.Helper()
	for _, e := range events {
		if e.Kind == EvErr {
			t.Fatalf("unexpected error event: %s", e.Text)
		}
	}
	return events
}

func TestParserStreamsProseThenTranscript(t *testing.T) {
	p := NewStreamParser(nil)
	if ev := p.Advance("Hello", false); len(ev) != 0 {
		t.Fatalf("early flush: %v", ev)
	}
	ev := drain(t, p.Advance("Hello. Who wri", false))
	want := []Event{{Kind: EvInk, Text: "Hello."}}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %v want %v", ev, want)
	}
	full := "Hello. Who writes to me? ⁂ it rained all night"
	ev = drain(t, p.Advance(full, true))
	want = []Event{
		{Kind: EvInk, Text: "Who writes to me?"},
		{Kind: EvTranscript, Text: "it rained all night"},
	}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %v want %v", ev, want)
	}
}

func TestParserRoutesShowDirective(t *testing.T) {
	p := NewStreamParser([]uint64{900, 800, 700})
	if ev := p.Advance("⟦sho", false); len(ev) != 0 {
		t.Fatalf("decided too early: %v", ev)
	}
	ev := drain(t, p.Advance("⟦show:2⟧", false))
	if !reflect.DeepEqual(ev, []Event{{Kind: EvShow, ID: 800}}) {
		t.Fatalf("got %v", ev)
	}
	full := "⟦show:2⟧\n⁂ show me the garden page"
	ev = drain(t, p.Advance(full, true))
	if !reflect.DeepEqual(ev, []Event{{Kind: EvTranscript, Text: "show me the garden page"}}) {
		t.Fatalf("got %v", ev)
	}
}

func TestParserShowToleratesSpacingAndCase(t *testing.T) {
	p := NewStreamParser([]uint64{42})
	ev := p.Advance("  ⟦Show: 1⟧", true)
	found := false
	for _, e := range ev {
		if e.Kind == EvShow && e.ID == 42 {
			found = true
		}
	}
	if !found {
		t.Fatalf("show not routed: %v", ev)
	}
}

func TestParserShowOutOfRangeIsError(t *testing.T) {
	p := NewStreamParser([]uint64{42})
	ev := p.Advance("⟦show:7⟧", true)
	if len(ev) == 0 || ev[0].Kind != EvErr {
		t.Fatalf("expected error, got %v", ev)
	}
}

func TestParserEmptyReplyIsError(t *testing.T) {
	p := NewStreamParser(nil)
	ev := p.Advance("", true)
	if len(ev) == 0 || ev[0].Kind != EvErr {
		t.Fatalf("expected error, got %v", ev)
	}
}

func TestParserWithoutSentinelStillFlushes(t *testing.T) {
	p := NewStreamParser(nil)
	ev := drain(t, p.Advance("A reply without postscript", true))
	if !reflect.DeepEqual(ev, []Event{{Kind: EvInk, Text: "A reply without postscript"}}) {
		t.Fatalf("got %v", ev)
	}
}

func TestParserDirectiveAfterProseIsStrippedNotInked(t *testing.T) {
	p := NewStreamParser([]uint64{900, 800})
	full := "Of course, let me show you. ⟦show:2⟧\n⁂ show me the rain"
	ev := drain(t, p.Advance(full, true))
	want := []Event{
		{Kind: EvInk, Text: "Of course, let me show you."},
		{Kind: EvTranscript, Text: "show me the rain"},
	}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %v want %v", ev, want)
	}
}

func TestStripDirectives(t *testing.T) {
	if got := stripDirectives("a ⟦show:1⟧ b"); got != "a b" {
		t.Fatalf("got %q", got)
	}
	if got := stripDirectives("plain text"); got != "plain text" {
		t.Fatalf("got %q", got)
	}
	if got := stripDirectives("tail ⟦show:2"); got != "tail" {
		t.Fatalf("got %q", got)
	}
}

func TestSentenceCut(t *testing.T) {
	if _, ok := sentenceCut("Hello", 0); ok {
		t.Fatal("no sentence yet")
	}
	cut, ok := sentenceCut("Hello. Who", 0)
	if !ok || cut != 6 {
		t.Fatalf("cut=%d ok=%v", cut, ok)
	}
}

func TestCleanFragmentStripsWrappingQuotes(t *testing.T) {
	if got := cleanFragment("  \"hello\"  "); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := cleanFragment("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}
