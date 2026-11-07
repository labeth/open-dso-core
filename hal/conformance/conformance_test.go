package conformance_test

import (
	"slices"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/conformance"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/hal/simboard"
	"github.com/labeth/open-dso-core/sample"
)

// The fake board keeps the HAL contract, as an 8-bit and a 12-bit scope.
// Its records always trigger, so the clauses that need a trigger that
// never fires are skipped.
// TRLC-LINKS: REQ-DSO-1001
func TestHalfake(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  halfake.Config
	}{
		{"2x8", halfake.Config{}},
		{"4x12", halfake.Config{Channels: 4, Format: sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := halfake.New(tc.cfg)
			conformance.Run(t, b, conformance.Options{
				BringUp:   true,
				NoHoldOff: "the fake board's records always trigger",
				CommitArms: func() int {
					n := 0
					for _, k := range b.ArmKinds() {
						if k == "commit" {
							n++
						}
					}
					return n
				},
				Inject: func(ev hal.InputEvent) bool { b.Inject(ev); return true },
			})
		})
	}
}

// The simulated board keeps the HAL contract, as the demo's two-channel
// 8-bit scope and as a four-channel 12-bit one, with a square on C1 that a
// trigger at the top of its level grid never reaches; the clause it is
// known to break is skipped with the defect.
// TRLC-LINKS: REQ-DSO-1001
func TestSimboard(t *testing.T) {
	disp := hal.DisplaySpec{W: 800, H: 480, Format: hal.RGB565, DivsH: 10, DivsV: 8}
	tb := hal.TimebaseSpec{TdivS: []float64{1e-6, 2e-6, 5e-6, 1e-5, 2e-5, 5e-5, 1e-4}}
	for _, tc := range []struct {
		name string
		p    hal.Profile
		cpd  float64
	}{
		{"2x8", hal.Profile{
			Channels: 2, Format: sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128},
			Rates: []hal.Rate{{SamplesPerS: 1e9, MaxChannels: 2}}, MaxDepth: 100000, Display: disp, Timebase: tb,
			Inputs: hal.InputSpec{Keys: []string{"run", "single", "auto"}, Knobs: []string{"tdiv"}},
		}, 25},
		{"4x12", hal.Profile{
			Channels: 4, Format: sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000},
			Rates: []hal.Rate{{SamplesPerS: 1e9, MaxChannels: 4}}, MaxDepth: 100000, Display: disp, Timebase: tb,
			Inputs: hal.InputSpec{Keys: []string{"run"}, Knobs: []string{"tdiv"}},
		}, 7680},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := simboard.New(simboard.Config{Profile: tc.p, CodesPerDiv: tc.cpd, FrameInterval: time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			if err := b.Signals().Set(0, "square,1e5,2,0"); err != nil {
				t.Fatal(err)
			}
			conformance.Run(t, b, conformance.Options{
				BringUp: true,
				Inject:  b.Inject,
				Skip: map[string]string{
					"input": "the simulated board accepts SetSound and Beep on a scope without a beeper, where the contract asks for a *hal.Refusal",
				},
			})
		})
	}
}

// Every clause is named once.
// TRLC-LINKS: REQ-DSO-1001
func TestClausesUnique(t *testing.T) {
	c := slices.Clone(conformance.Clauses)
	slices.Sort(c)
	if len(slices.Compact(c)) != len(conformance.Clauses) {
		t.Fatalf("a clause is listed twice: %v", conformance.Clauses)
	}
}
