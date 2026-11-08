package simboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/engine/fanout"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/simboard"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/scopeweb"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/webtest"
)

// webScope is the core's web scope over a simulated board run by the core
// engine's HAL loop; it runs until the test ends.
func webScope(t *testing.T, p hal.Profile) webserver.Scope {
	t.Helper()
	b, err := simboard.New(simboard.Config{Profile: p, FrameInterval: 5 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Signals().Set(0, "square,1e5,2,0"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Platform().BringUp(t.Context()); err != nil {
		t.Fatal(err)
	}
	st := instrument.State{TimeDivS: 10e-6, Running: true}
	for range p.Channels {
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
	web := scopeweb.New(scopeweb.Config{Board: b, Instrument: m, Frames: fo, Version: "test"}).WithCaps()
	if fo.WaitNext(0, 3*time.Second) == 0 {
		t.Fatal("no frame published")
	}
	return web
}

// The core's web scope over the simulated board passes the web server's
// conformance suite, as a two-channel 8-bit scope and as a four-channel
// 12-bit one.
// TRLC-LINKS: REQ-DSO-1001, REQ-DSO-161, REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func TestWebConformance(t *testing.T) {
	disp := hal.DisplaySpec{W: 800, H: 480, Format: hal.RGB565, DivsH: 10, DivsV: 8}
	for _, tc := range []struct {
		name string
		p    hal.Profile
	}{
		{"2x8", hal.Profile{
			Channels: 2, Format: sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}, Display: disp, Stage: 2,
			Timebase: hal.TimebaseSpec{TdivS: []float64{1e-6, 1e-5, 1e-4}},
		}},
		{"4x12", hal.Profile{
			Channels: 4, Format: sample.CodeFormat{Bits: 12, Storage: sample.U16, Shift: 4, Min: 0x0800, Max: 0xF7F0, Mid: 0x8000},
			Display: disp, Stage: 1, Timebase: hal.TimebaseSpec{TdivS: []float64{1e-6, 1e-5, 1e-4}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			webtest.Run(t, func() webserver.Scope { return webScope(t, tc.p) })
		})
	}
}
