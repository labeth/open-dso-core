// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb_test

import (
	"context"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/engine/fanout"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/scopeweb"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/webtest"
)

// format12 is 12-bit codes left-aligned in 16-bit words about 0x8000.
var format12 = sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000}

// rig is a whole in-memory scope: a fake board run by the core engine's
// HAL loop, its frames fanned out, the instrument model and the web scope
// over them.
type rig struct {
	b   *halfake.Board
	m   *instrument.Model
	fo  *fanout.Fanout[sample.Frame]
	web webserver.Scope
}

// newRig starts a scope of cfg's fake board; it runs until the test ends.
func newRig(t *testing.T, cfg halfake.Config) *rig {
	t.Helper()
	b := halfake.New(cfg)
	if _, err := b.Platform().BringUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	n := b.Profile().Channels
	st := instrument.State{TimeDivS: 1e-6, Running: true, Trigger: instrument.Edge{Source: hal.Source{Kind: hal.Analog}, Slope: hal.Rising}}
	for range n {
		st.Channels = append(st.Channels, hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6})
	}
	m := instrument.New(st, nil)
	fo := fanout.NewFrames()
	m.SetFrames(fo)
	loop, cycle := engine.NewHALLoop(engine.HALConfig{Board: b, Instrument: m, WaitSlice: 5 * time.Millisecond, IdleWait: 5 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan struct{})
	done := make(chan struct{}, 2)
	go func() { loop.Run(ctx); done <- struct{}{} }()
	go func() { fo.Run(cycle, stop); done <- struct{}{} }()
	t.Cleanup(func() {
		cancel()
		close(stop)
		<-done
		<-done
	})
	r := &rig{b: b, m: m, fo: fo}
	r.web = scopeweb.New(scopeweb.Config{Board: b, Instrument: m, Frames: fo, Version: "test", Product: "open-dso test"}).WithCaps()
	if fo.WaitNext(0, 2*time.Second) == 0 {
		t.Fatal("no frame published")
	}
	return r
}

// The web scope over a two-channel 8-bit fake and a four-channel 12-bit
// one passes the conformance suite: the first serves frame wire v1 and v2,
// the second v2 only.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func TestConformance(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  halfake.Config
	}{
		{"2x8", halfake.Config{}},
		{"4x12", halfake.Config{Channels: 4, Format: format12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			webtest.Run(t, func() webserver.Scope { return newRig(t, tc.cfg).web })
		})
	}
}
