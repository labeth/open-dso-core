// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB
package scopeweb_test

import (
	"encoding/json"
	"testing"

	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/sample"
)

// TRLC-LINKS: REQ-DSO-164
func TestHardwareCounterStatus(t *testing.T) {
	r := newRig(t, halfake.Config{})
	read := func() map[string]json.RawMessage {
		t.Helper()
		data, err := json.Marshal(r.web.Status())
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]json.RawMessage
		if err := json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, ok := read()["counter"]; ok {
		t.Fatal("counter advertised on a board without one")
	}
	r.m.SetHardwareCounter(2, sample.Counters{FreqHz: 25e3, Valid: true})
	out := read()
	var text string
	if err := json.Unmarshal(out["counter_text"], &text); err != nil {
		t.Fatal(err)
	}
	if text != "Counter C3 25 kHz" {
		t.Fatalf("counter text %q", text)
	}
	var counter struct {
		Source int
		FreqHz float64 `json:"freq_hz"`
		Valid  bool
	}
	if err := json.Unmarshal(out["counter"], &counter); err != nil {
		t.Fatal(err)
	}
	if counter.Source != 2 || counter.FreqHz != 25e3 || !counter.Valid {
		t.Fatalf("counter: %+v", counter)
	}
}
