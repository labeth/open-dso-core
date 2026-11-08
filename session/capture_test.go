package session

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/labeth/open-dso-core/measure"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// A scope that serves frame wire v1 alone is captured all the same: its
// raw record (8-bit codes or Q8.8 words) is kept in v2 with its scale,
// and its shown frame's m1 and m2 are its reported measurements.
// TRLC-LINKS: REQ-DSO-295
func TestCaptureOfV1Scope(t *testing.T) {
	hdr := wire.FrameReply{Seq: 7, EdgeX: 2.5, SampleS: 2e-9, Cols: 4, Vpc1: 0.04, Vpc2: 0.08, Off1V: 0.1}
	raw := wire.EncodeRawFrame(wire.RawFrame{Header: hdr, Payload: []byte{128, 153, 178, 128, 100, 110, 120, 130}})
	v2, err := rawAsV2(raw)
	if err != nil {
		t.Fatal(err)
	}
	f, err := frameOfV2(v2)
	if err != nil {
		t.Fatal(err)
	}
	if f.Seq != 7 || f.SampleS != 2e-9 || f.TriggerAt != 2.5 || f.Len() != 4 || len(f.Traces) != 2 {
		t.Fatalf("frame %+v", f)
	}
	if got := f.Traces[0].Codes8; string(got) != string([]byte{128, 153, 178, 128}) {
		t.Errorf("C1 codes %v", got)
	}
	wf, err := wire.DecodeFrame2(v2)
	if err != nil {
		t.Fatal(err)
	}
	if !wf.Header.Raw || wf.Traces[1].Scale.PerCodeV != 0.08 || wf.Traces[0].Scale.OffV != 0.1 {
		t.Errorf("v2 header %+v, C2 scale %+v", wf.Header, wf.Traces[1].Scale)
	}

	words := make([]byte, 8)
	for i, w := range []uint16{128 << 8, 130<<8 | 0x90} {
		binary.LittleEndian.PutUint16(words[2*i:], w)
		binary.LittleEndian.PutUint16(words[4+2*i:], w)
	}
	hdr.Cols = 2
	v2, err = rawAsV2(wire.EncodeRawFrame(wire.RawFrame{Header: hdr, Precision: true, Payload: words}))
	if err != nil {
		t.Fatal(err)
	}
	if wf, err = wire.DecodeFrame2(v2); err != nil {
		t.Fatal(err)
	}
	if got := wf.Traces[0].Codes.U16; len(got) != 2 || got[1] != 130<<8|0x90 || wf.Traces[0].Fmt.Frac != 8 {
		t.Errorf("precision words %v, format %+v", got, wf.Traces[0].Fmt)
	}

	shown := wire.EncodeBinFrame(wire.FrameReply{
		Seq: 7, Cols: 2, C1: []int16{1, 2}, C2: []int16{3, 4},
		M1: &measure.Result{Vpp: 3.3, Overshoot: 11}, M2: &measure.Result{Vpp: 1},
	})
	if headerSeq(shown) != 7 {
		t.Errorf("v1 seq %d", headerSeq(shown))
	}
	s := Snapshot{}
	allMeasures(s, shown)
	if v, ok := numOf(s["fmeas.ch1.vpp"]); !ok || v != 3.3 {
		t.Errorf("ch1 vpp %v", s["fmeas.ch1.vpp"])
	}
	if v, ok := numOf(s["fmeas.ch1.overshoot"]); !ok || v != 11 {
		t.Errorf("ch1 overshoot %v", s["fmeas.ch1.overshoot"])
	}
	if v, ok := numOf(s["fmeas.ch2.vpp"]); !ok || v != 1 {
		t.Errorf("ch2 vpp %v", s["fmeas.ch2.vpp"])
	}
}

// What a scope measured on its edge windows, which its raw record does not
// hold, is left out of a replay's comparison while the replay has no such
// windows (its flag unset), and compared like the rest once it has; every
// other field is compared exactly.
// TRLC-LINKS: REQ-DSO-295
func TestEdgeWindowFieldsOfAReplay(t *testing.T) {
	f := &CaptureFile{Reported: Snapshot{
		"fmeas.ch1.edge_shape": {V: true}, "fmeas.ch1.rise_s": num(4e-9), "fmeas.ch1.overshoot": num(13),
		"fmeas.ch1.freq": num(1e5), "fmeas.ch2.edge_shape": {V: false}, "fmeas.ch2.rise_s": num(0),
	}}
	got := Snapshot{
		"fmeas.ch1.edge_shape": {V: false}, "fmeas.ch1.rise_s": num(0), "fmeas.ch1.overshoot": num(0),
		"fmeas.ch1.freq": num(1e5), "fmeas.ch2.edge_shape": {V: false}, "fmeas.ch2.rise_s": num(0),
	}
	want := []string{"fmeas.ch1.edge_shape", "fmeas.ch1.overshoot", "fmeas.ch1.rise_s"}
	if w := f.FromEdgeWindows(got); !slices.Equal(w, want) {
		t.Fatalf("edge-window fields %v, want %v", w, want)
	}
	if bad := f.Mismatches(got); len(bad) != 0 {
		t.Fatalf("mismatches %v", bad)
	}
	got["fmeas.ch1.freq"] = num(2e5)
	if bad := f.Mismatches(got); len(bad) != 1 {
		t.Fatalf("a record's own field differing: mismatches %v", bad)
	}
	got["fmeas.ch1.freq"], got["fmeas.ch1.edge_shape"] = num(1e5), Value{V: true}
	if w, bad := f.FromEdgeWindows(got), f.Mismatches(got); len(w) != 0 || len(bad) != 2 {
		t.Fatalf("a replay with edge windows: skipped %v, mismatches %v", w, bad)
	}
}
