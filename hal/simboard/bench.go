package simboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/siggen"
)

// BenchFeature names the bench's routes in the capabilities.
const BenchFeature = "sim-bench"

// screenWait is how long a request for the next picture waits.
const screenWait = time.Second

// bench is the simulated board's own web routes: the generator that feeds
// its inputs, its front panel's keys, knobs and touches, its lamps and its
// screen. They are the bench around the scope, so a page can drive the
// board as a hand drives a scope; nothing in them reaches the product past
// the HAL.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) bench() hal.Extension {
	return hal.Extension{Feature: BenchFeature, Routes: []hal.Route{
		{Pattern: "GET /api/sim/state", Handler: http.HandlerFunc(b.hState)},
		{Pattern: "POST /api/sim/signal", Handler: http.HandlerFunc(b.hSignal)},
		{Pattern: "POST /api/sim/bus", Handler: http.HandlerFunc(b.hBus)},
		{Pattern: "POST /api/sim/input", Handler: http.HandlerFunc(b.hInput)},
		{Pattern: "GET /api/sim/screen.rgba", Handler: http.HandlerFunc(b.hScreen)},
	}}
}

// reply writes v as JSON.
// TRLC-LINKS: REQ-DSO-1001
func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v) // a client gone mid-reply is not the board's fault
}

// fail answers a request the bench cannot carry out.
// TRLC-LINKS: REQ-DSO-1001
func fail(w http.ResponseWriter, err error) {
	reply(w, map[string]any{"ok": false, "err": err.Error()})
}

// decode reads a small JSON body into v.
// TRLC-LINKS: REQ-DSO-1001
func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(v); err != nil {
		return fmt.Errorf("bad JSON: %w", err)
	}
	return nil
}

// hState is the bench's state: the lit lamps, each input's signal, the
// screen's picture number.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hState(w http.ResponseWriter, _ *http.Request) {
	b.screen.mu.Lock()
	seq := b.screen.seq
	b.screen.mu.Unlock()
	reply(w, map[string]any{
		"ok": true, "lamps": nonNil(b.Lamps()), "signals": b.sig.Specs(), "channels": b.profile.Channels,
		"screen": map[string]any{"w": b.profile.Display.W, "h": b.profile.Display.H, "seq": seq, "touch": b.profile.Display.Touch},
		"keys":   nonNil(b.profile.Inputs.Keys), "knobs": nonNil(b.profile.Inputs.Knobs),
	})
}

// nonNil is s, or an empty list for nil, so the JSON is a list.
// TRLC-LINKS: REQ-DSO-1001
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// inputOf is an input by its 1-based number or its name ("C2").
// TRLC-LINKS: REQ-DSO-1001
func inputOf(v json.RawMessage, n int) (int, error) {
	var s string
	if json.Unmarshal(v, &s) != nil {
		s = string(v)
	}
	s = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(s)), "C")
	i, err := strconv.Atoi(s)
	if err != nil || i < 1 || i > n {
		return 0, fmt.Errorf("input %s: want C1 … C%d", string(v), n)
	}
	return i - 1, nil
}

// hSignal sets one input's signal ({"ch":"C1","spec":"sine,1e3,1,0"}) or
// several at once ({"signals":{"C1":"…","C2":"…"}}).
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hSignal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ch      json.RawMessage   `json:"ch"`
		Spec    string            `json:"spec"`
		Signals map[string]string `json:"signals"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	specs := map[int]string{}
	if len(req.Ch) > 0 {
		in, err := inputOf(req.Ch, b.sig.Inputs())
		if err != nil {
			fail(w, err)
			return
		}
		specs[in] = req.Spec
	}
	for name, spec := range req.Signals {
		in, err := inputOf(json.RawMessage(strconv.Quote(name)), b.sig.Inputs())
		if err != nil {
			fail(w, err)
			return
		}
		specs[in] = spec
	}
	if err := b.sig.SetMany(specs); err != nil {
		fail(w, err)
		return
	}
	reply(w, map[string]any{"ok": true, "signals": b.sig.Specs()})
}

// hBus lays a protocol bus out on inputs: {"kind":"spi","lines":{"clk":"C1",
// "mosi":"C2"},"rate":1e6,"vpp":3.3,"offset":1.65,"opts":{"data":"a53c"}}.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hBus(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string                     `json:"kind"`
		Lines  map[string]json.RawMessage `json:"lines"`
		Rate   float64                    `json:"rate"`
		Vpp    float64                    `json:"vpp"`
		Offset float64                    `json:"offset"`
		Opts   map[string]string          `json:"opts"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	bus := siggen.Bus{Kind: req.Kind, Lines: map[string]int{}, Rate: req.Rate, Vpp: req.Vpp, Offset: req.Offset, Opts: req.Opts}
	for line, ch := range req.Lines {
		in, err := inputOf(ch, b.sig.Inputs())
		if err != nil {
			fail(w, err)
			return
		}
		bus.Lines[line] = in
	}
	if err := b.sig.SetBus(bus); err != nil {
		fail(w, err)
		return
	}
	reply(w, map[string]any{"ok": true, "signals": b.sig.Specs()})
}

// hInput presses a key ({"key":"run"}), turns a knob ({"knob":"tdiv",
// "steps":-1}) or touches the screen ({"touch":{"x":100,"y":200}}), as the
// panel's hand does: the event goes to the board's input queue.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Knob  string `json:"knob"`
		Steps int    `json:"steps"`
		Touch *struct {
			X, Y    int
			Gesture string
		} `json:"touch"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	var evs []hal.InputEvent
	switch {
	case req.Key != "":
		evs = []hal.InputEvent{
			{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: req.Key, Down: true},
			{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: req.Key},
		}
	case req.Knob != "" && req.Steps != 0:
		evs = []hal.InputEvent{{Source: hal.FromPanel, Kind: hal.KnobEvent, Knob: req.Knob, Steps: max(-100, min(100, req.Steps))}}
	case req.Touch != nil:
		if !b.profile.Display.Touch {
			fail(w, errors.New("this scope has no touch screen"))
			return
		}
		g := req.Touch.Gesture
		if g == "" {
			g = "tap"
		}
		evs = []hal.InputEvent{{Source: hal.FromTouch, Kind: hal.TouchEvent, TouchX: req.Touch.X, TouchY: req.Touch.Y, Gesture: g, Down: true}}
	default:
		fail(w, errors.New(`want {"key":…}, {"knob":…,"steps":n} or {"touch":{"x":…,"y":…}}`))
		return
	}
	for _, ev := range evs {
		if !b.Inject(ev) {
			fail(w, errors.New("the panel's queue is full"))
			return
		}
	}
	reply(w, map[string]any{"ok": true})
}

// hScreen is the next picture as raw RGBA rows (?after=N waits up to a
// second for a picture past N); its number, width and height are headers.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hScreen(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	px, wd, ht, seq := b.Screen(after, screenWait)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Screen-Seq", strconv.FormatUint(seq, 10))
	h.Set("X-Screen-W", strconv.Itoa(wd))
	h.Set("X-Screen-H", strconv.Itoa(ht))
	_, _ = w.Write(px) // a client gone mid-reply is not the board's fault
}
