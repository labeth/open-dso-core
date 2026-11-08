package simdemo_test

import (
	"testing"

	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/session"
	"github.com/labeth/open-dso-core/simdemo/demotest"
)

// A captured record replayed through the demo reports what the scope
// reported of it: the frame's measurements and the packets its record
// decodes to, exactly. The test scope's captures are made on its own demo
// (UPDATE_DEMO_FIXTURES=1 writes them); a scope's repository replays its
// bench captures the same way.
// TRLC-LINKS: REQ-DSO-295
func TestReplayCaptures(t *testing.T) {
	profile, demo, _ := fixture(t)
	demotest.Captures(t, profile, demo, "testdata/captures", []demotest.Recipe{
		{
			Name: "square-1mhz", About: "C1: a 1 MHz square, 2 Vpp about 0 V",
			Signals: map[string]session.Signal{"C1": {Kind: "square", Hz: 1e6, Vpp: 2}},
			Rows:    map[string]string{"time.tdiv": "1us", "ch1.vdiv": "500mV"},
		},
		{
			Name: "uart-115200", About: "C1: UART 115200 8N1 sending 48 69 20 55 aa 0f f0 0a, 3.3 V logic",
			Signals: map[string]session.Signal{"C1": {Kind: "uart", Rate: 115200, Vpp: 3.3, Offset: 1.65, Hex: "48692055aa0ff00a"}},
			Rows:    map[string]string{"time.tdiv": "100us", "ch1.vdiv": "1V", "trig.level": "1.65V"},
			Decode:  &session.CaptureDecode{Proto: framecalc.ProtoUART, Baud: 115200, ChA: 0},
		},
	})
}
