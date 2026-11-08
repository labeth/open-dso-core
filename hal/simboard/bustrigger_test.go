package simboard

import (
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/prototrig"
	"github.com/labeth/open-dso-core/siggen"
)

// busSpec is the protocol trigger on request p, its lines sliced at level.
func busSpec(p prototrig.SerialParams, level float64) hal.TriggerSpec {
	return hal.TriggerSpec{Sweep: hal.NormalSweep, Stages: []hal.TrigStage{{
		Node: hal.NodeProtocol, Source: hal.Source{Kind: hal.Analog, Index: p.ChA}, Levels: hal.Levels{High: level},
		Proto: &prototrig.Bus{Request: p, Levels: [2]float64{level, level}},
	}}}
}

// The protocol trigger fires on a generated bus's frame start as a scope's
// receiver does: every record of a NORMAL sweep is triggered, none forced.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-1001
func TestBusTriggerFiresOnFrames(t *testing.T) {
	cases := []struct {
		name string
		bus  siggen.Bus
		req  prototrig.SerialParams
		tdiv float64
	}{
		{
			"spi frame start",
			siggen.Bus{Kind: "spi", Lines: map[string]int{"clk": 0, "mosi": 1}, Rate: 1e6, Vpp: 3.3, Offset: 1.65, Opts: map[string]string{"data": "a53c"}},
			prototrig.SerialParams{Proto: engine.SerSPI, ChA: 0, ChB: 1, MSB: true, Baud: 115200, Pattern: []prototrig.PatternElem{{Kind: 1}, {Kind: 2}}},
			2e-6,
		},
		{
			"uart byte 6c",
			siggen.Bus{Kind: "uart", Lines: map[string]int{"tx": 0}, Rate: 115200, Vpp: 3.3, Offset: 1.65, Opts: map[string]string{"data": "48656c6c6f"}},
			prototrig.SerialParams{Proto: engine.SerUART, ChA: 0, ChB: 1, Baud: 115200, Bits: 8, Bytes: []int{0x6c}},
			20e-6,
		},
		{
			"i2c start",
			siggen.Bus{Kind: "i2c", Lines: map[string]int{"scl": 0, "sda": 1}, Rate: 100e3, Vpp: 3.3, Offset: 1.65, Opts: map[string]string{"data": "00a5"}},
			prototrig.SerialParams{Proto: engine.SerI2C, ChA: 0, ChB: 1, Addr: -1, RW: 2},
			50e-6,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBoard(t, 2, format8, 25)
			if err := b.Signals().SetBus(tc.bus); err != nil {
				t.Fatal(err)
			}
			c := hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6}
			for range 3 {
				_, st, err := acquire(t, b, c, hal.Request{TimePerDivS: tc.tdiv}, busSpec(tc.req, 1.65), 2*time.Second)
				if err != nil || !st.Triggered || st.Forced {
					t.Fatalf("status %+v, %v", st, err)
				}
			}
		})
	}
}
