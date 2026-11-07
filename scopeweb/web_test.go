// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb_test

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/measure"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/scopeweb"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// serve runs one request against the rig's server.
func (r *rig) serve(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	webserver.New(r.web).Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body)))
	return rec
}

// json serves one request and decodes its JSON reply.
func (r *rig) json(t *testing.T, method, path, body string) map[string]any {
	t.Helper()
	rec := r.serve(t, method, path, body)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s: %d %v (%s)", method, path, rec.Code, err, rec.Body)
	}
	return out
}

// set posts one control and wants it applied.
func (r *rig) set(t *testing.T, body string) map[string]any {
	t.Helper()
	out := r.json(t, http.MethodPost, "/api/set", body)
	if out["ok"] != true {
		t.Fatalf("%s: %v", body, out)
	}
	return out
}

// frame2 fetches the frame after since in wire v2.
func (r *rig) frame2(t *testing.T, since uint64) wire.Frame {
	t.Helper()
	rec := r.serve(t, http.MethodGet, "/api/frame.bin?v=2&waitms=1000&since="+jsonNum(since), "")
	f, err := wire.DecodeFrame2(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("frame: %d %v", rec.Code, err)
	}
	return f
}

func jsonNum(v any) string { b, _ := json.Marshal(v); return string(b) }

// num is a JSON number (NaN for anything else).
func num(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return math.NaN()
}

// settled waits until a frame shows what fn wants (the owner commits a
// change at its next commit point).
func (r *rig) settled(t *testing.T, fn func(wire.Frame) bool) wire.Frame {
	t.Helper()
	var f wire.Frame
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		f = r.frame2(t, f.Header.Seq)
		if !f.Header.Unchanged && fn(f) {
			return f
		}
	}
	t.Fatalf("no frame settled: %+v", f.Header)
	return f
}

// swing is a trace's lowest and highest volts.
func swing(tr *wire.FrameTrace) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for i := range tr.Codes.Len() {
		v := tr.Volts(tr.Codes, i)
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	return lo, hi
}

// near reports a within 1 % of b.
func near(a, b float64) bool { return math.Abs(a-b) <= 0.01*math.Max(math.Abs(b), 1e-3) }

// A four-channel 12-bit scope: v2 frames decode to the fake's ±2-division
// square on every channel; settings v2 bodies and the v1 names of C1/C2
// reach the board through the instrument model and the frames follow;
// what is not on the ladder is refused with why; the status reads back.
// TRLC-LINKS: REQ-DSO-162, REQ-DSO-163, REQ-DSO-164
func TestSettingsRoundTrip(t *testing.T) {
	r := newRig(t, halfake.Config{Channels: 4, Format: format12})
	f := r.settled(t, func(f wire.Frame) bool { return len(f.Traces) == 4 })
	for i := range f.Traces {
		tr := &f.Traces[i]
		if tr.Fmt.Bits != 12 || tr.Fmt.Storage != "u16" || tr.Meas == nil {
			t.Fatalf("trace %d: %+v", i, tr.TraceHeader)
		}
		if lo, hi := swing(tr); !near(lo, -2) || !near(hi, 2) {
			t.Fatalf("C%d swings %g … %g V, want ±2 V", i+1, lo, hi)
		}
		if !near(tr.Meas.Vpp, 4) {
			t.Fatalf("C%d measured Vpp %g, want 4", i+1, tr.Meas.Vpp)
		}
	}
	if out := r.set(t, `{"control":"vdiv","ch":3,"value":0.5}`); out["applied"] != 0.5 {
		t.Fatalf("vdiv C4: %v", out)
	}
	r.set(t, `{"control":"vdiv2","value":0.2}`)
	r.set(t, `{"control":"offset","ch":2,"value":0.25}`)
	f = r.settled(t, func(f wire.Frame) bool {
		_, hi3 := swing(&f.Traces[3])
		_, hi1 := swing(&f.Traces[1])
		return near(hi3, 1) && near(hi1, 0.4)
	})
	if lo, hi := swing(&f.Traces[2]); !near(lo, -2.25) || !near(hi, 1.75) {
		t.Fatalf("C3 with a 0.25 V offset swings %g … %g V", lo, hi)
	}
	if got := r.b.FrontEnd().Get(3).VoltsDiv; got != 0.5 {
		t.Fatalf("the board's C4 is at %g V/div", got)
	}
	for _, body := range []string{
		`{"control":"vdiv","ch":1,"value":0.3}`,
		`{"control":"vdiv","ch":4,"value":1}`,
		`{"control":"probe","ch":0,"value":7}`,
		`{"control":"trigtype","value":2}`,
		`{"control":"triglevelcode","value":1000}`,
		`{"control":"zonemode","value":1}`,
		`{"control":"tdiv","value":3e-6}`,
	} {
		if out := r.json(t, http.MethodPost, "/api/set", body); out["ok"] != false || out["err"] == "" {
			t.Fatalf("%s: %v", body, out)
		}
	}
	r.set(t, `{"control":"trigsource","value":2}`)
	if out := r.set(t, `{"control":"triglevel","ch":2,"value_v":0.5}`); !near(num(out["applied"]), 0.5) {
		t.Fatalf("triglevel: %v", out)
	}
	if out := r.json(t, http.MethodPost, "/api/set", `{"control":"triglevel","ch":1,"value_v":0.5}`); out["ok"] != false {
		t.Fatalf("a level for a channel that is not the source: %v", out)
	}
	r.set(t, `{"control":"tdiv","value":1e-3}`)
	r.set(t, `{"control":"coupling","ch":1,"value":1}`)
	st := r.json(t, http.MethodGet, "/api/status", "")
	chans, _ := st["channels"].([]any)
	c2, _ := chans[1].(map[string]any)
	if st["vdiv4"] != 0.5 || st["vdiv2"] != 0.2 || st["tdiv_set_s"] != 1e-3 || st["trig_source"] != 2.0 ||
		!near(num(st["trig_volts"]), 0.5) || len(chans) != 4 || c2["coupling"] != "AC" || st["caps_gen"] == nil {
		t.Fatalf("status: %v", st)
	}
	if s := r.m.State(); s.Trigger.Source != (hal.Source{Kind: hal.Analog, Index: 2}) || s.Channels[1].Coupling != hal.AC {
		t.Fatalf("instrument: %+v", s)
	}
}

// The capabilities of a four-channel 12-bit scope come from its HAL: four
// channels, its code format, its ladders and couplings, frame wire v2 only,
// the shared controls, and the parts it lacks with the profile's reasons.
// TRLC-LINKS: REQ-DSO-164
func TestCapsFromHAL(t *testing.T) {
	r := newRig(t, halfake.Config{Channels: 4, Format: format12})
	var caps wire.Caps
	if err := json.Unmarshal(r.serve(t, http.MethodGet, "/api/caps", "").Body.Bytes(), &caps); err != nil {
		t.Fatal(err)
	}
	if len(caps.Channels) != 4 || caps.Channels[3].Name != "C4" || caps.Codes.Bits != 12 || caps.Codes.Storage != "u16" ||
		caps.Codes.Mid != 0x8000 || caps.Codes.CodesPerDiv != float64(0xF7F0-0x0800)/10 {
		t.Fatalf("channels and codes: %+v %+v", caps.Channels, caps.Codes)
	}
	if len(caps.Wire.Frame) != 1 || caps.Wire.Frame[0] != 2 || len(caps.Vertical.Vdivs) != 12 || caps.Vertical.Couplings[0] != "DC" ||
		len(caps.Timebase.Tdivs) != 4 || caps.Trigger.Types[0] != "edge" || len(caps.Trigger.Sources) != 4 {
		t.Fatalf("wire %v vertical %+v timebase %+v trigger %+v", caps.Wire, caps.Vertical, caps.Timebase, caps.Trigger)
	}
	if f := caps.Features["stacker"]; f.OK || f.Why != "the fake board has no stacker" {
		t.Fatalf("stacker: %+v", f)
	}
	if f := caps.Features["mask"]; f.OK || f.Why == "" {
		t.Fatalf("mask: %+v", f)
	}
	ok := false
	for _, c := range caps.Controls {
		ok = ok || (c.Name == "triglevel" && c.Unit == "V")
	}
	if !ok || caps.Screen.W != 64 || caps.Limits.LiveClients != 4 {
		t.Fatalf("controls %+v screen %+v limits %+v", caps.Controls, caps.Screen, caps.Limits)
	}
	if rec := r.serve(t, http.MethodGet, "/api/frame.bin?cols=100", ""); rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), string(wire.FrameMagic2[:])) {
		t.Fatalf("a v1 request to a v2-only scope: %d %.20q", rec.Code, rec.Body)
	}
}

// A record of a four-channel scope names its channels and carries each
// one's codes on the 8-bit view with its scale.
// TRLC-LINKS: REQ-DSO-280
func TestRecordChannels(t *testing.T) {
	r := newRig(t, halfake.Config{Channels: 4, Format: format12})
	r.set(t, `{"control":"memdepth","value":4000}`)
	rec := r.serve(t, http.MethodGet, "/api/record.bin?sample_s=1&wait_ms=3000", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("record: %d %s", rec.Code, rec.Body)
	}
	b := rec.Body.Bytes()
	n := int(binary.LittleEndian.Uint32(b))
	var h wire.RecordReply
	if err := json.Unmarshal(b[4:4+n], &h); err != nil {
		t.Fatal(err)
	}
	if len(h.Chans) != 4 || h.Chans[3] != 3 || len(h.Vpc) != 4 || h.N < 1024 || len(b) != 4+n+4*h.N {
		t.Fatalf("header %+v, %d bytes", h, len(b))
	}
	codes := b[4+n+3*h.N:]
	lo, hi := 255, 0
	for _, c := range codes {
		lo, hi = min(lo, int(c)), max(hi, int(c))
	}
	if v := (float64(hi)-128)*h.Vpc[3] - h.Off[3]; !near(v, 2) {
		t.Fatalf("C4's record peaks at %g V", v)
	}
	_ = lo
}

// A two-channel 8-bit scope serves v1 frames too: the same square in the
// v1 codes about 128 at the v1 scales, and its record as v1 carries it.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-280
func TestV1Scope(t *testing.T) {
	r := newRig(t, halfake.Config{})
	rec := r.serve(t, http.MethodGet, "/api/frame.bin?cols=100&waitms=500&full=1", "")
	b := rec.Body.Bytes()
	if rec.Code != http.StatusOK || len(b) < 8 || b[0] != wire.BinMagic {
		t.Fatalf("v1 frame: %d", rec.Code)
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	var h wire.FrameReply
	if err := json.Unmarshal(b[8:8+n], &h); err != nil {
		t.Fatal(err)
	}
	if !near(h.Vpc1, 1/25.5) || h.M1 == nil || !near(h.M1.Vpp, 4) {
		t.Fatalf("v1 header vpc1 %g meas %+v", h.Vpc1, h.M1)
	}
	out := r.json(t, http.MethodGet, "/api/record.bin?sample_s=1&wait_ms=10", "")
	if out["err"] == nil {
		t.Fatalf("a record of too few samples: %v", out)
	}
}

// Setups keep the instrument state in the board's setup slots and recall
// it.
// TRLC-LINKS: REQ-DSO-219
func TestSetups(t *testing.T) {
	r := newRig(t, halfake.Config{Channels: 4, Format: format12})
	r.set(t, `{"control":"vdiv","ch":2,"value":0.2}`)
	if out := r.json(t, http.MethodPost, "/api/setup", `{"op":"save","slot":3}`); out["ok"] != true {
		t.Fatalf("save: %v", out)
	}
	r.set(t, `{"control":"vdiv","ch":2,"value":2}`)
	if out := r.json(t, http.MethodPost, "/api/setup", `{"op":"recall","slot":3}`); out["ok"] != true {
		t.Fatalf("recall: %v", out)
	}
	if got := r.m.State().Channels[2].VoltsDiv; got != 0.2 {
		t.Fatalf("recalled C3 at %g V/div", got)
	}
	out := r.json(t, http.MethodGet, "/api/setup", "")
	slots, _ := out["slots"].([]any)
	s3, _ := slots[2].(map[string]any)
	if s3["used"] != true || !strings.Contains(jsonNum(s3["summary"]), "C3 0.2V") {
		t.Fatalf("slots: %v", out)
	}
	if out := r.json(t, http.MethodPost, "/api/setup", `{"op":"recall","slot":4}`); out["ok"] != false {
		t.Fatalf("an empty slot: %v", out)
	}
}

// BuildReply measures once per frame, coupling and scale and reuses the
// result on repeat calls; a scale change measures afresh at once, a new
// frame within 100 ms only while running free.
// TRLC-LINKS: REQ-DSO-164
func TestMeasCacheReuse(t *testing.T) {
	c1, c2 := make([]uint8, 512), make([]uint8, 512)
	for i := range c1 {
		if i%100 < 50 {
			c1[i], c2[i] = 60, 70
		} else {
			c1[i], c2[i] = 200, 210
		}
	}
	f := &sample.Frame{Seq: 21, WinPoints: 512, TriggerAt: 256, SampleS: 1e-6, Traces: []sample.Trace{{Codes8: c1, Valid: 512}, {Ch: 1, Codes8: c2, Valid: 512}}}
	s := scopeweb.New(scopeweb.Config{Frames: scopeweb.NewFanout()})
	reply := func(vpc0 float64, free bool) *measure.Result {
		return s.BuildReply(f, scopeweb.ReplyQuery{Cols: 512, Vpc: [2]float64{vpc0, 1. / 32}, PosFrac: 0.5, MeasThrottle: free}).M1
	}
	r1, r2 := reply(1./32, true), reply(1./32, true)
	if r1 == nil || r1 != r2 {
		t.Fatalf("cache miss on identical inputs: %p vs %p", r1, r2)
	}
	r3 := reply(1./16, true)
	if r3 == r1 {
		t.Fatal("a scale change did not measure afresh")
	}
	f.Seq = 22
	if r4 := reply(1./16, true); r4 != r3 {
		t.Fatal("a new frame within the throttle window should reuse the measurements")
	}
	f.Seq = 23
	r5 := reply(1./16, false)
	if r5 == r3 {
		t.Fatal("a new frame without the throttle did not measure afresh")
	}
	f.Seq = 24
	s.ExpireMeasurements()
	if r6 := reply(1./16, true); r6 == r5 {
		t.Fatal("an expired throttle window did not measure afresh")
	}
}

// zoneRecorder keeps the zones installed.
type zoneRecorder struct{ got []engine.Zone }

func (z *zoneRecorder) SetZones(zs []engine.Zone) { z.got = zs }

// Zones in volts become display codes at the channel's scale; a zone on a
// channel the zone qualifier does not compare is refused with why; a
// failing frame reads in the v2 shape with its traces' scales.
// TRLC-LINKS: REQ-DSO-162
func TestZonesVolts(t *testing.T) {
	r := newRig(t, halfake.Config{})
	zr := &zoneRecorder{}
	r.web = scopeweb.New(scopeweb.Config{Frames: r.fo, Scales: fixedScales{}, Parts: scopeweb.Parts{Zones: zr}})
	out := r.json(t, http.MethodPost, "/api/zones", `[{"dt_lo_s":-1e-6,"dt_hi_s":1e-6,"v_lo":-0.5,"v_hi":1,"ch":1}]`)
	if out["ok"] != true || len(zr.got) != 1 || zr.got[0].CodeLo != 120 || zr.got[0].CodeHi != 150 || zr.got[0].Ch != 1 {
		t.Fatalf("%v %+v", out, zr.got)
	}
	if out := r.json(t, http.MethodPost, "/api/zones", `[{"dt_lo_s":0,"dt_hi_s":1,"v_lo":0,"v_hi":1,"ch":2}]`); out["ok"] != false || out["err"] == "" {
		t.Fatalf("a zone on C3: %v", out)
	}
	if out := r.json(t, http.MethodPost, "/api/zones", `[{"dt_lo_s":0,"dt_hi_s":1,"code_lo":10,"code_hi":20,"ch":3}]`); out["ok"] != true || zr.got[0].Ch != 1 {
		t.Fatalf("a v1 zone: %v %+v", out, zr.got)
	}
}

// fixedScales are 0.05 V a code with a 0.1 V offset on both channels.
type fixedScales struct{}

func (fixedScales) Scales() (off, vpc [2]float64) {
	return [2]float64{0.1, 0.1}, [2]float64{0.05, 0.05}
}
func (fixedScales) Coupling(int) int                { return 0 }
func (fixedScales) Probe(int) float64               { return 1 }
func (fixedScales) Inverted() [2]bool               { return [2]bool{} }
func (fixedScales) Position() (float64, bool, bool) { return 0.5, true, false }
