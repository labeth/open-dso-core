package keytable_test

import (
	"fmt"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/keytable"
)

// table is a panel with a press-only key, a pressed-and-released one, a
// channel key, a shared vertical knob and push, a one-detent knob and an
// accelerated one.
var table = keytable.Table{
	Keys: []keytable.Key{
		{Panel: "run", Name: "run"},
		{Panel: "default", Name: "defaultsetup", Release: true},
		{Panel: "ch1", Name: "ch1", Release: true, Selects: 1},
		{Panel: "ch2", Name: "ch2", Release: true, Selects: 2},
		{Panel: "vscale-push", Name: "ch{ch}vdivpush", Release: true},
		{Panel: "roll"},
	},
	Knobs: []keytable.Knob{
		{Panel: "vscale", Name: "ch{ch}vdiv", Max: 255},
		{Panel: "tdiv", Name: "tdiv", Max: 1},
		{Panel: "pos", Name: "ch1pos", Steps: []int{1, 2, 3, 50, 100}},
	},
}

// A key sends a press alone or with its release as its board does, a
// channel key selects the channel a shared control acts on, and a key the
// board gives no name sends nothing.
// TRLC-LINKS: REQ-DSO-1001
func TestPress(t *testing.T) {
	cases := []struct {
		panel string
		sel   int
		want  string
		after int
	}{
		{"run", 1, "[run↓]", 1},
		{"default", 1, "[defaultsetup↓ defaultsetup↑]", 1},
		{"ch2", 1, "[ch2↓ ch2↑]", 2},
		{"vscale-push", 2, "[ch2vdivpush↓ ch2vdivpush↑]", 2},
		{"roll", 1, "[]", 1},
	}
	for _, c := range cases {
		k, _ := table.Key(c.panel)
		evs, after := k.Press(c.sel)
		if got := show(evs); got != c.want || after != c.after {
			t.Errorf("%s with C%d: %s, C%d selected; want %s, C%d", c.panel, c.sel, got, after, c.want, c.after)
		}
	}
}

// A turn goes out in the steps one event of its knob carries.
// TRLC-LINKS: REQ-DSO-1001
func TestTurn(t *testing.T) {
	cases := []struct {
		panel   string
		detents int
		want    string
	}{
		{"tdiv", -3, "[tdiv-1 tdiv-1 tdiv-1]"},
		{"vscale", 300, "[ch1vdiv+255 ch1vdiv+45]"},
		{"pos", 54, "[ch1pos+50 ch1pos+3 ch1pos+1]"},
	}
	for _, c := range cases {
		k, _ := table.Knob(c.panel)
		if got := show(k.Turn(c.detents, 1)); got != c.want {
			t.Errorf("%s %+d: %s, want %s", c.panel, c.detents, got, c.want)
		}
	}
}

// Check refuses an event the board never sends, and a name looked up by
// the UI model's name follows the selected channel.
// TRLC-LINKS: REQ-DSO-1001
func TestCheck(t *testing.T) {
	key := func(n string, down bool) hal.InputEvent {
		return hal.InputEvent{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: n, Down: down}
	}
	knob := func(n string, s int) hal.InputEvent {
		return hal.InputEvent{Source: hal.FromPanel, Kind: hal.KnobEvent, Knob: n, Steps: s}
	}
	for _, c := range []struct {
		ev  hal.InputEvent
		sel int
		ok  bool
	}{
		{key("run", true), 1, true},
		{key("run", false), 1, false}, // press only
		{key("defaultsetup", false), 1, true},
		{key("ch2vdivpush", true), 2, true},
		{key("ch2vdivpush", true), 1, false}, // the knob acts on C1
		{key("menu", true), 1, false},
		{knob("tdiv", 2), 1, false},
		{knob("tdiv", -1), 1, true},
		{knob("ch1pos", 4), 1, false},
		{knob("ch1pos", -50), 1, true},
		{knob("ch1vdiv", 255), 1, true},
	} {
		if err := table.Check(c.ev, c.sel); (err == nil) != c.ok {
			t.Errorf("%+v with C%d: %v, want ok %v", c.ev, c.sel, err, c.ok)
		}
	}
	keys, knobs := table.Names()
	if fmt.Sprint(keys) != "[ch1 ch1vdivpush ch2 ch2vdivpush defaultsetup run]" || fmt.Sprint(knobs) != "[ch1pos ch1vdiv ch2vdiv tdiv]" {
		t.Errorf("names %v %v", keys, knobs)
	}
	if err := table.CheckProfile(hal.InputSpec{Keys: keys, Knobs: knobs[1:]}); err == nil {
		t.Error("a profile without ch1pos passed")
	}
}

// show is events as text: a key's press ↓ and release ↑, a knob's steps.
func show(evs []hal.InputEvent) string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		switch {
		case e.Kind == hal.KnobEvent:
			out = append(out, fmt.Sprintf("%s%+d", e.Knob, e.Steps))
		case e.Down:
			out = append(out, e.Key+"↓")
		default:
			out = append(out, e.Key+"↑")
		}
	}
	return fmt.Sprint(out)
}
