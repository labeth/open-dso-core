package simboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/keytable"
	"github.com/labeth/open-dso-core/sample"
)

// The bench's hand makes the events the board's key table says: a press
// alone where the panel reports no release, a turn split into the steps
// one event carries, a shared knob refused for a channel not selected
// until its key is pressed.
// TRLC-LINKS: REQ-DSO-1001
func TestHandFollowsTheKeyTable(t *testing.T) {
	b, err := New(Config{
		Profile: hal.Profile{
			Channels: 2, Format: sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128},
			Inputs: hal.InputSpec{Keys: []string{"run", "ch1", "ch2", "ch1vdivpush", "ch2vdivpush"}, Knobs: []string{"tdiv", "ch1vdiv", "ch2vdiv"}},
		},
		KeyTable: keytable.Table{
			Keys: []keytable.Key{
				{Panel: "run", Name: "run"},
				{Panel: "ch1", Name: "ch1", Release: true, Selects: 1},
				{Panel: "ch2", Name: "ch2", Release: true, Selects: 2},
				{Panel: "vpush", Name: "ch{ch}vdivpush", Release: true},
				{Panel: "nav"},
			},
			Knobs: []keytable.Knob{{Panel: "tdiv", Name: "tdiv", Max: 1}, {Panel: "vscale", Name: "ch{ch}vdiv", Max: 255}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := http.NewServeMux()
	for _, r := range b.bench().Routes {
		h.Handle(r.Pattern, r.Handler)
	}
	post := func(body string) (string, []string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/sim/input", strings.NewReader(body)))
		var evs []string
		for {
			select {
			case ev := <-b.events:
				if ev.Kind == hal.KnobEvent {
					evs = append(evs, fmt.Sprintf("%s%+d", ev.Knob, ev.Steps))
				} else {
					evs = append(evs, fmt.Sprintf("%s:%v", ev.Key, ev.Down))
				}
				continue
			case <-time.After(10 * time.Millisecond):
			}
			return strings.TrimSpace(rec.Body.String()), evs
		}
	}
	for _, c := range []struct{ body, want string }{
		{`{"key":"run"}`, "[run:true]"},
		{`{"panel":"run"}`, "[run:true]"},
		{`{"knob":"tdiv","steps":-3}`, "[tdiv-1 tdiv-1 tdiv-1]"},
		{`{"panel":"vscale","detents":4}`, "[ch1vdiv+4]"},
		{`{"knob":"ch2vdiv","steps":1}`, "[]"}, // C1 is selected
		{`{"panel":"ch2"}`, "[ch2:true ch2:false]"},
		{`{"knob":"ch2vdiv","steps":1}`, "[ch2vdiv+1]"},
		{`{"panel":"vpush"}`, "[ch2vdivpush:true ch2vdivpush:false]"},
		{`{"panel":"nav"}`, "[]"},
	} {
		reply, evs := post(c.body)
		if got := fmt.Sprint(evs); got != c.want {
			t.Errorf("%s: %s (%s), want %s", c.body, got, reply, c.want)
		}
	}
	if reply, _ := post(`{"knob":"ch2vdiv","steps":1}`); !strings.Contains(reply, `"ok":true`) {
		t.Errorf("C2 selected: %s", reply)
	}
}
