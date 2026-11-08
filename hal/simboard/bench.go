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
	"github.com/labeth/open-dso-core/hal/keytable"
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
		{Pattern: "POST /api/sim/replay", Handler: http.HandlerFunc(b.hReplay)},
		{Pattern: "DELETE /api/sim/replay", Handler: http.HandlerFunc(b.hReplay)},
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
		"keytable": b.keys, "selected": b.selected(),
	})
}

// selected is the channel the panel's shared controls act on (1-based).
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) selected() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return max(b.sel, 1)
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

// hInput is a hand on the panel: it presses a key ({"panel":"run"}) or
// turns a knob ({"panel":"tdiv","detents":-1}) by the panel's own names,
// presses a key or turns a knob by the UI model's name the panel sends
// ({"key":"run"}, {"knob":"tdiv","steps":-1}), or touches the screen
// ({"touch":{"x":100,"y":200}}, a tap; with "gesture" down, move
// or up and a contact "id", one report of a finger). The board's key table (Config.KeyTable)
// makes the events the real panel makes: a press alone or with its
// release, a turn split into the steps one event of that knob carries, a
// shared knob acting on the channel whose key was pressed last.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hInput(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Panel   string `json:"panel"`
		Detents int    `json:"detents"`
		Key     string `json:"key"`
		Knob    string `json:"knob"`
		Steps   int    `json:"steps"`
		Touch   *struct {
			X, Y    int
			Gesture string
			ID      int `json:"id"`
		} `json:"touch"`
	}
	if err := decode(r, &req); err != nil {
		fail(w, err)
		return
	}
	evs, err := b.inputEvents(req.Panel, req.Detents, req.Key, req.Knob, req.Steps)
	switch {
	case err != nil:
		fail(w, err)
		return
	case req.Touch != nil:
		if !b.profile.Display.Touch {
			fail(w, errors.New("this scope has no touch screen"))
			return
		}
		evs = touchEvents(req.Touch.X, req.Touch.Y, req.Touch.ID, req.Touch.Gesture)
		if evs == nil {
			fail(w, fmt.Errorf("touch gesture %q: want tap, down, move or up", req.Touch.Gesture))
			return
		}
	case evs == nil && req.Panel == "" && req.Key == "" && req.Knob == "":
		fail(w, errors.New(`want {"panel":…} or {"panel":…,"detents":n}, {"key":…}, {"knob":…,"steps":n} or {"touch":{"x":…,"y":…}}`))
		return
	}
	for _, ev := range evs {
		if !b.Inject(ev) {
			fail(w, errors.New("the panel's queue is full"))
			return
		}
	}
	reply(w, map[string]any{"ok": true, "events": len(evs)})
}

// touchEvents are the events of a finger on the screen as a touch screen
// reports them: a tap (the default) goes down and up at (x, y); "down",
// "move" and "up" are one report of contact id. Nil for another gesture.
// TRLC-LINKS: REQ-DSO-1001, REQ-DSO-299
func touchEvents(x, y, id int, gesture string) []hal.InputEvent {
	ev := hal.InputEvent{Source: hal.FromTouch, Kind: hal.TouchEvent, TouchX: x, TouchY: y, Contact: id, Down: true}
	switch gesture {
	case "", "tap":
		up := ev
		up.Down = false
		return []hal.InputEvent{ev, up}
	case "down":
		return []hal.InputEvent{ev}
	case "move":
		ev.Gesture = hal.TouchMove
		return []hal.InputEvent{ev}
	case "up":
		ev.Down = false
		return []hal.InputEvent{ev}
	}
	return nil
}

// inputEvents are the events a hand on control panel (detents for a knob)
// or on the control that sends key or knob makes, through the key table;
// none for no control.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) inputEvents(panel string, detents int, key, knob string, steps int) ([]hal.InputEvent, error) {
	t := b.keys
	b.mu.Lock()
	defer b.mu.Unlock()
	press := func(k keytable.Key) []hal.InputEvent {
		evs, sel := k.Press(b.sel)
		b.sel = sel
		return evs
	}
	turn := func(k keytable.Knob, n int) ([]hal.InputEvent, error) {
		if n == 0 {
			return nil, fmt.Errorf("%s: a turn of no detents", k.Panel)
		}
		return k.Turn(max(-1000, min(1000, n)), b.sel), nil
	}
	switch {
	case panel != "":
		if k, ok := t.Key(panel); ok {
			return press(k), nil
		}
		if k, ok := t.Knob(panel); ok {
			return turn(k, detents)
		}
		return nil, fmt.Errorf("%s: this panel has no such control", panel)
	case key != "":
		k, err := t.KeyNamed(key, b.sel)
		if err != nil {
			return nil, err
		}
		return press(k), nil
	case knob != "":
		k, err := t.KnobNamed(knob, b.sel)
		if err != nil {
			return nil, err
		}
		return turn(k, steps)
	}
	return nil, nil
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
