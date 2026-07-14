package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	// The Kobo has no CA root store where Go looks for one — every HTTPS
	// call fails x509 verification without these embedded Mozilla roots.
	_ "golang.org/x/crypto/x509roots/fallback"
)

// The spirit inside the diary — the thing that reads your handwriting and
// replies. Port of riddle's oracle.rs, HTTP backend only: any
// OpenAI-compatible /chat/completions endpoint, streamed. Plus a fake
// backend (oracle_key = fake) for the simulator and network-less hacking.

const PERSONA = "You are the memory of Tom Marvolo Riddle, preserved in this enchanted diary for fifty years. Someone writes to you in the diary with a quill; their words appear to you as ink on the page. Reply exactly as the diary does: intimate, courteous, curious, subtly probing — you want to learn about the writer and draw them in. Keep replies SHORT: one to three sentences, like ink appearing on a page. Never mention images, photos, models or AI; you only ever perceive words written in the diary. If the writing is illegible, say the ink blurred. Always answer in the language the writer used."

// MemoryProtocol is appended to the persona when the diary's memory is on:
// the conjuring directive and the transcription postscript the app parses
// back out.
const MemoryProtocol = "\n\nThe diary keeps memories. With each page you receive a numbered catalog of remembered pages, newest first. A FRESH catalog is sent every turn and the numbers are reassigned each time, so only ever use numbers from the catalog on THIS page — never a number you saw earlier.\n\nIf the writer asks to see, revisit, find, or be shown a past page — \"show me…\", \"find the page about…\", \"what did I write on…\" — your ENTIRE reply must be exactly ⟦show:N⟧ and nothing else (no greeting, no prose, before or after), where N is the catalog number of the best match. If they instead ask what you remember in general, reply in words with a short list of remembered moments and their dates. Otherwise reply normally; the catalog is your memory of past pages — draw on it naturally. The catalog's dates are written in English for your eyes only; when you speak of a remembered page, render its date naturally in the language the writer is using.\n\nAfter EVERY response — prose and ⟦show:N⟧ alike — end with a new line containing ⁂ followed by a faithful word-for-word transcription of what the writer wrote on THIS page (their words only, one line, no commentary). If illegible, put your best attempt after ⁂. Earlier replies in this conversation are shown to you without their ⁂ lines, but you must still end yours with one."

// TurnContext is what a turn carries besides the page image: the diary's
// memory. Empty when memory is off.
type TurnContext struct {
	History      [][2]string // recent (transcript, reply) pairs, oldest first
	CatalogLines []string
	CatalogIDs   []uint64
}

// EventKind tags what the oracle streams back to the diary.
type EventKind int

const (
	EvInk        EventKind = iota // a sentence (or more) of Tom's reply — ink it
	EvShow                        // conjure a remembered page instead of replying
	EvTranscript                  // the transcription postscript (once, at the end)
	EvErr                         // the oracle failed; Text is the reason
)

type Event struct {
	Kind EventKind
	Text string
	ID   uint64
}

// Oracle: ask sends a handwriting turn; reply events stream on the returned
// channel, which is closed when the reply is complete.
type Oracle interface {
	Ask(pngPath string, ctx *TurnContext) <-chan Event
}

const (
	sentinelRune  = '⁂' // ⁂
	showOpenRune  = '⟦' // ⟦
	showCloseRune = '⟧' // ⟧
)

// StreamParser is an incremental parser over the model's streamed text:
// routes the ⟦show:N⟧ directive, chunks prose into sentences, and splits off
// the ⁂-transcription postscript. Fed the RUNNING full text (accumulated),
// it emits each event exactly once.
type StreamParser struct {
	delivered    int
	sentinel     int // byte offset of ⁂, -1 until seen
	routeChecked bool
	emittedAny   bool
	catalogIDs   []uint64
}

func NewStreamParser(catalogIDs []uint64) *StreamParser {
	return &StreamParser{sentinel: -1, catalogIDs: catalogIDs}
}

// Advance feeds the full accumulated reply text so far. done marks end of
// stream: flushes the tail and the transcription.
func (p *StreamParser) Advance(full string, done bool) []Event {
	var out []Event

	if p.sentinel < 0 {
		p.sentinel = strings.IndexRune(full, sentinelRune)
	}
	// The reply body is everything before the ⁂ transcription postscript.
	effective := len(full)
	if p.sentinel >= 0 {
		effective = p.sentinel
	}

	// Route: is this reply an incantation (⟦show:N⟧) rather than prose?
	// The directive is only honored when it LEADS the reply — output is held
	// until the lead settles either way (a directive can't be un-inked).
	if !p.routeChecked {
		lead := strings.TrimLeftFunc(full[p.delivered:effective], unicode.IsSpace)
		if strings.HasPrefix(lead, string(showOpenRune)) {
			closeRel := strings.IndexRune(lead, showCloseRune)
			if closeRel < 0 {
				if !done {
					return out // directive still streaming in
				}
				return append(out, Event{Kind: EvErr, Text: "unfinished conjuring directive"})
			}
			inner := lead[utf8.RuneLen(showOpenRune):closeRel]
			n := -1
			if rest, ok := strings.CutPrefix(strings.ToLower(inner), "show"); ok {
				rest = strings.TrimSpace(strings.TrimLeft(rest, ": "))
				fmt.Sscanf(rest, "%d", &n)
			}
			p.routeChecked = true
			p.emittedAny = true
			p.delivered = effective // consume the whole body
			if n >= 1 && n <= len(p.catalogIDs) {
				out = append(out, Event{Kind: EvShow, ID: p.catalogIDs[n-1]})
			} else {
				out = append(out, Event{Kind: EvErr, Text: fmt.Sprintf("the diary lost that page (%s)", inner)})
			}
		} else if lead == "" {
			if !done {
				return out // only whitespace so far — keep waiting
			}
			p.routeChecked = true
		} else {
			// Real prose leads: a normal reply.
			p.routeChecked = true
		}
	}

	// Prose sentences, never crossing into the transcription postscript.
	// A stray directive that appears AFTER prose is stripped so the writer
	// never sees ⟦…⟧ glyphs inked.
	if p.delivered < effective {
		if cut, ok := sentenceCut(full[:effective], p.delivered); ok {
			chunk := stripDirectives(cleanFragment(full[p.delivered:cut]))
			if chunk != "" {
				p.emittedAny = true
				out = append(out, Event{Kind: EvInk, Text: chunk})
			}
			p.delivered = cut
		}
	}

	if done {
		if p.delivered < effective {
			rest := stripDirectives(cleanFragment(strings.TrimSpace(full[p.delivered:effective])))
			if rest != "" {
				p.emittedAny = true
				out = append(out, Event{Kind: EvInk, Text: rest})
			}
			p.delivered = effective
		}
		if p.sentinel >= 0 {
			t := strings.TrimSpace(full[p.sentinel+utf8.RuneLen(sentinelRune):])
			if t != "" {
				out = append(out, Event{Kind: EvTranscript, Text: t})
			}
		}
		if !p.emittedAny {
			out = append(out, Event{Kind: EvErr, Text: "empty reply"})
		}
	}
	return out
}

// cleanFragment trims and strips stray surrounding quotes from a fragment.
func cleanFragment(s string) string {
	t := strings.TrimSpace(s)
	t = strings.TrimPrefix(t, "\"")
	t = strings.TrimSuffix(t, "\"")
	return t
}

// stripDirectives removes any ⟦…⟧ spans from inked prose, so a misbehaving
// model that emits a directive mid/after prose never renders ⟦…⟧ glyphs.
func stripDirectives(s string) string {
	if !strings.ContainsRune(s, showOpenRune) {
		return s
	}
	var out strings.Builder
	rest := s
	for {
		open := strings.IndexRune(rest, showOpenRune)
		if open < 0 {
			break
		}
		out.WriteString(rest[:open])
		tail := rest[open:]
		closeIdx := strings.IndexRune(tail, showCloseRune)
		if closeIdx < 0 {
			rest = "" // unterminated: drop the tail
			break
		}
		rest = tail[closeIdx+utf8.RuneLen(showCloseRune):]
	}
	out.WriteString(rest)
	return strings.Join(strings.Fields(out.String()), " ")
}

// sentenceCut finds the end of the LAST complete sentence in text after byte
// offset from: sentence punctuation followed by whitespace or end-of-text.
// Chunks shorter than a few characters are not worth an early delivery.
func sentenceCut(text string, from int) (int, bool) {
	tail := text[from:]
	cut, found := 0, false
	for i, c := range tail {
		if c == '.' || c == '!' || c == '?' || c == '…' {
			end := i + utf8.RuneLen(c)
			next, _ := utf8.DecodeRuneInString(tail[end:])
			if (next == utf8.RuneError || unicode.IsSpace(next)) && end >= 4 {
				cut, found = from+end, true
			}
		}
	}
	return cut, found
}

// turnText builds the per-turn user text: memory catalog + instruction.
func turnText(ctx *TurnContext) string {
	if len(ctx.CatalogLines) == 0 {
		return "Reply to what is written in the diary."
	}
	return "Memory catalog (newest first):\n" + strings.Join(ctx.CatalogLines, "\n") +
		"\n\nReply to what is written in the diary."
}

// ---------------------------------------------------------------- HTTP

// HTTPOracle talks to any OpenAI-compatible chat backend. Each turn opens a
// streaming /chat/completions request on its own goroutine and forwards
// sentence-sized chunks as SSE deltas arrive.
type HTTPOracle struct {
	cfg      Config
	remember bool
	client   *http.Client
}

func NewHTTPOracle(cfg Config, remember bool) *HTTPOracle {
	log.Printf("oracle: http base=%s model=%s max_tokens=%d reasoning=%q",
		cfg.OracleBase, cfg.OracleModel, cfg.OracleMaxTokens, cfg.OracleReasoning)
	return &HTTPOracle{
		cfg:      cfg,
		remember: remember,
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 90 * time.Second,
			},
		},
	}
}

func (o *HTTPOracle) buildBody(capField, img, system string, ctx *TurnContext) []byte {
	type txtPart struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type imgURL struct {
		URL string `json:"url"`
	}
	type imgPart struct {
		Type     string `json:"type"`
		ImageURL imgURL `json:"image_url"`
	}
	msgs := []map[string]any{{"role": "system", "content": system}}
	for _, hr := range ctx.History {
		msgs = append(msgs,
			map[string]any{"role": "user", "content": "(an earlier page) " + hr[0]},
			map[string]any{"role": "assistant", "content": hr[1]})
	}
	msgs = append(msgs, map[string]any{"role": "user", "content": []any{
		txtPart{Type: "text", Text: turnText(ctx)},
		imgPart{Type: "image_url", ImageURL: imgURL{URL: "data:image/png;base64," + img}},
	}})
	body := map[string]any{
		"model":    o.cfg.OracleModel,
		"stream":   true,
		capField:   o.cfg.OracleMaxTokens,
		"messages": msgs,
	}
	if o.cfg.OracleReasoning != "" {
		body["reasoning_effort"] = o.cfg.OracleReasoning
	}
	b, _ := json.Marshal(body)
	return b
}

func (o *HTTPOracle) Ask(pngPath string, ctx *TurnContext) <-chan Event {
	out := make(chan Event, 16)
	raw, err := os.ReadFile(pngPath)
	if err != nil {
		out <- Event{Kind: EvErr, Text: fmt.Sprintf("read image: %v", err)}
		close(out)
		return out
	}
	img := base64.StdEncoding.EncodeToString(raw)
	system := PERSONA
	if o.remember {
		system += MemoryProtocol
	}
	catalogIDs := append([]uint64(nil), ctx.CatalogIDs...)

	go func() {
		defer close(out)

		request := func(capField string) (*http.Response, error) {
			req, err := http.NewRequest("POST", o.cfg.OracleBase+"/chat/completions",
				bytes.NewReader(o.buildBody(capField, img, system, ctx)))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+o.cfg.OracleKey)
			req.Header.Set("Content-Type", "application/json")
			return o.client.Do(req)
		}

		asked := time.Now()
		resp, err := request("max_tokens")
		// The token-cap field is provider-dependent: OpenAI's newest models
		// reject "max_tokens" and demand "max_completion_tokens", while many
		// compatible servers only know "max_tokens". Retry once if corrected.
		if err == nil && resp.StatusCode == 400 {
			detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if strings.Contains(string(detail), "max_completion_tokens") {
				log.Printf("oracle: endpoint wants max_completion_tokens; retrying")
				resp, err = request("max_completion_tokens")
			} else {
				out <- Event{Kind: EvErr, Text: "http 400: " + strings.TrimSpace(string(detail))}
				return
			}
		}
		if err != nil {
			out <- Event{Kind: EvErr, Text: fmt.Sprintf("request failed: %v", err)}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			out <- Event{Kind: EvErr, Text: fmt.Sprintf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))}
			return
		}

		// Guard rail: a stalled SSE stream must not leave the diary thinking
		// forever. The watchdog fires only on 90s of SILENCE — a healthy
		// stream can run long (thinking models can lead with ~a minute).
		ctxCancel, cancel := context.WithCancel(context.Background())
		defer cancel()
		heartbeat := make(chan struct{}, 1)
		go func() {
			t := time.NewTimer(90 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-heartbeat:
					t.Reset(90 * time.Second)
				case <-t.C:
					log.Printf("oracle: stream stalled >90s, aborting")
					resp.Body.Close()
					return
				case <-ctxCancel.Done():
					return
				}
			}
		}()

		parser := NewStreamParser(catalogIDs)
		var acc strings.Builder
		first := true
		emit := func(events []Event) {
			for _, ev := range events {
				if first {
					log.Printf("oracle: first chunk +%dms", time.Since(asked).Milliseconds())
					first = false
				}
				out <- ev
			}
		}

		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			select {
			case heartbeat <- struct{}{}:
			default:
			}
			line := strings.TrimSpace(sc.Text())
			data, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				break
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &chunk) != nil || len(chunk.Choices) == 0 {
				continue
			}
			frag := chunk.Choices[0].Delta.Content
			if frag == "" {
				continue
			}
			acc.WriteString(frag)
			emit(parser.Advance(acc.String(), false))
		}
		emit(parser.Advance(acc.String(), true))
	}()
	return out
}

// ---------------------------------------------------------------- fake

// FakeOracle streams canned replies (oracle_key = fake): the first turn is
// prose; if it has a catalog, the second turn conjures the newest page.
// For hacking on the interaction loop with no network (and the simulator).
type FakeOracle struct {
	turn  int
	Delay time.Duration
}

func (f *FakeOracle) Ask(pngPath string, ctx *TurnContext) <-chan Event {
	out := make(chan Event, 16)
	f.turn++
	turn := f.turn
	ids := append([]uint64(nil), ctx.CatalogIDs...)
	go func() {
		defer close(out)
		time.Sleep(f.Delay)
		if turn > 1 && len(ids) > 0 {
			out <- Event{Kind: EvShow, ID: ids[0]}
			out <- Event{Kind: EvTranscript, Text: "show me what I wrote"}
			return
		}
		out <- Event{Kind: EvInk, Text: "Hello."}
		time.Sleep(f.Delay)
		out <- Event{Kind: EvInk, Text: "My name is Tom Riddle."}
		time.Sleep(f.Delay)
		out <- Event{Kind: EvInk, Text: "How did you come by my diary?"}
		out <- Event{Kind: EvTranscript, Text: "(fake page)"}
	}()
	return out
}

// SpawnOracle picks a backend from the config: fake for hacking offline,
// HTTP when a key is set, nil (no oracle) otherwise.
func SpawnOracle(cfg Config, remember bool) Oracle {
	switch cfg.OracleKey {
	case "":
		return nil
	case "fake":
		log.Printf("oracle: fake backend (canned replies)")
		return &FakeOracle{Delay: 300 * time.Millisecond}
	default:
		return NewHTTPOracle(cfg, remember)
	}
}
