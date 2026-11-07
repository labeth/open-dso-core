// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package wire_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/testenv"
	"github.com/labeth/open-dso-core/webclient"
	"github.com/labeth/open-dso-core/webserver/webtest"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// decoded is one frame as binframe2.test.cjs prints it: typed arrays as
// {type, v}, every other field as it is.
type decoded map[string]json.RawMessage

// array is a decoded typed array.
type array struct {
	Type string    `json:"type"`
	V    []float64 `json:"v"`
}

// arr reads field k as a typed array.
// TRLC-LINKS: REQ-SDS-163
func (d decoded) arr(t *testing.T, k string) array {
	t.Helper()
	var a array
	if err := json.Unmarshal(d[k], &a); err != nil {
		t.Fatalf("%s: %v (%s)", k, err, d[k])
	}
	return a
}

// num reads field k as a number.
// TRLC-LINKS: REQ-SDS-163
func (d decoded) num(t *testing.T, k string) float64 {
	t.Helper()
	var v float64
	if err := json.Unmarshal(d[k], &v); err != nil {
		t.Fatalf("%s: %v (%s)", k, err, d[k])
	}
	return v
}

// twin8 is a two-channel 8-bit frame as a sample frame, at 25 codes a
// division about 128, and its v1 reply.
// TRLC-LINKS: REQ-SDS-163
func twin8(precision bool) (*sample.Frame, wire.FrameReply) {
	c1, c2 := []uint8{100, 128, 156, 200, 7}, []uint8{10, 20, 30, 40, 250}
	q1, q2 := []uint16{100 << 8, 128<<8 + 1, 156 << 8, 200<<8 + 255, 7 << 8}, []uint16{10 << 8, 20 << 8, 30<<8 + 128, 40 << 8, 250 << 8}
	f := &sample.Frame{Seq: 4, Content: sample.RawFullRate, TriggerAt: 2}
	if precision {
		f.Flags = sample.Precision
	}
	rep := wire.FrameReply{Seq: 4, Cols: 5, EdgeX: 2, Vpc1: 0.04, Vpc2: 0.02, Off1V: 0.1, Off2V: -0.05}
	for ch, w := range [][]uint8{c1, c2} {
		f.Traces = append(f.Traces, sample.Trace{
			Ch: ch, Format: sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128},
			Codes8: w, Precision: [][]uint16{q1, q2}[ch], PrecisionFrac: 8, Valid: 5, CodesPerDiv: 25,
			Scale: sample.Scale{Zero: 128, PerCode: []float64{0.04, 0.02}[ch], OffsetV: []float64{0.1, -0.05}[ch]},
		})
	}
	for _, v := range c1 {
		rep.C1 = append(rep.C1, int16(v))
	}
	for _, v := range c2 {
		rep.C2 = append(rep.C2, int16(v))
	}
	if precision {
		rep.Q1, rep.Q2, rep.FractionBits = q1, q2, 8
	}
	return f, rep
}

// encode2 is a sample frame as wire v2 with the page's shaping fields set.
// TRLC-LINKS: REQ-SDS-163
func encode2(t *testing.T, f *sample.Frame) []byte {
	t.Helper()
	var w wire.Frame
	w.FromSample(f)
	w.Header.Cols, w.Header.EdgeX = f.Traces[0].Valid, f.TriggerAt
	b, err := wire.EncodeFrame2(&w)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The Go encoder's v2 frames decode in the page's binframe2.js to the
// frame object the page draws: a two-channel 8-bit frame and its Q8.8
// precision words exactly as their v1 twins decode, and four channels of
// 12-bit left-aligned words to display units that keep every code.
// TRLC-LINKS: REQ-SDS-163
func TestFrameV2NodeRoundTrip(t *testing.T) {
	testenv.NeedNode(t)
	dir := t.TempDir()
	write := func(name string, b []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []bool{false, true} {
		f, rep := twin8(p)
		tag := map[bool]string{false: "codes", true: "precision"}[p]
		write(tag+"_v1.bin", wire.EncodeBinFrame(rep))
		write(tag+"_v2.bin", encode2(t, f))
	}
	wide := webtest.WideRecord(3, 600)
	write("wide_v2.bin", encode2(t, wide))

	cmd := exec.CommandContext(t.Context(), "node", "binframe2.test.cjs", dir)
	cmd.Dir = webclient.Dir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	got := map[string]decoded{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 16<<20)
	for sc.Scan() {
		var line struct {
			Name  string  `json:"name"`
			Frame decoded `json:"frame"`
		}
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Fatalf("%v: %.200s", err, sc.Bytes())
		}
		if line.Frame == nil {
			t.Fatalf("%s did not decode", line.Name)
		}
		got[line.Name] = line.Frame
	}

	for _, tag := range []string{"codes", "precision"} {
		one, two := got[tag+"_v1.bin"], got[tag+"_v2.bin"]
		for _, k := range []string{"c1", "c2"} {
			a, b := one.arr(t, k), two.arr(t, k)
			if a.Type != b.Type || len(a.V) != len(b.V) {
				t.Fatalf("%s %s: v1 %s[%d], v2 %s[%d]", tag, k, a.Type, len(a.V), b.Type, len(b.V))
			}
			for i := range a.V {
				if a.V[i] != b.V[i] {
					t.Fatalf("%s %s[%d]: v1 %g, v2 %g", tag, k, i, a.V[i], b.V[i])
				}
			}
		}
		for _, k := range []string{"vpc1", "vpc2", "off1_v", "off2_v", "seq", "cols", "edge_x"} {
			if a, b := one.num(t, k), two.num(t, k); math.Abs(a-b) > 1e-15 {
				t.Fatalf("%s %s: v1 %g, v2 %g", tag, k, a, b)
			}
		}
	}

	w := got["wide_v2.bin"]
	for ch, tr := range wide.Traces {
		n := ch + 1
		c := w.arr(t, "c"+string(rune('0'+n)))
		if c.Type != "Float32Array" || len(c.V) != tr.Valid {
			t.Fatalf("C%d: %s[%d]", n, c.Type, len(c.V))
		}
		for i, word := range tr.Codes16 {
			want := 128 + (float64(word)-0x8000)*25/7680
			if math.Abs(c.V[i]-want) > 1e-4 {
				t.Fatalf("C%d[%d]: %g, want %g", n, i, c.V[i], want)
			}
			// one 12-bit code is 25/480 display units: every code stays apart
			if i > 0 && word != tr.Codes16[i-1] && c.V[i] == c.V[i-1] {
				t.Fatalf("C%d[%d]: two codes drawn alike", n, i)
			}
		}
		if vpc := w.num(t, "vpc"+string(rune('0'+n))); math.Abs(vpc-tr.Scale.PerCode*7680/25) > 1e-15 {
			t.Fatalf("C%d volts per unit %g", n, vpc)
		}
	}
}
