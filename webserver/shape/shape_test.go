// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package shape

import (
	"os/exec"
	"reflect"
	"testing"

	"github.com/labeth/open-dso-core/testenv"
	"github.com/labeth/open-dso-core/webclient"
)

// nodeRun runs a node harness of the web client in its directory.
// TRLC-LINKS: REQ-SDS-071
func nodeRun(t *testing.T, harness string) ([]byte, error) {
	cmd := exec.CommandContext(t.Context(), "node", harness)
	cmd.Dir = webclient.Dir()
	return cmd.CombinedOutput()
}

// TRLC-LINKS: REQ-SDS-163
func TestWindowMapping(t *testing.T) {
	sig := make([]uint8, 100)
	for i := range sig {
		sig[i] = uint8(i)
	}

	// Window exactly covering the record: nearest-sample mode.
	out := Window(sig, 100, 100, -1, false, 10, 0.5)
	for x, v := range out {
		if want := int16(x * 10); v != want {
			t.Fatalf("out[%d] = %d, want %d", x, v, want)
		}
	}

	// Window wider than the record: clamp to the full record and fill every
	// column with real data (no gaps) rather than leave off-record blanks.
	out = Window(sig, 100, 200, -1, false, 10, 0.5)
	for x, v := range out {
		if v < 0 {
			t.Fatalf("column %d is a gap; a too-wide window should show the full record", x)
		}
	}
	if out[0] != 0 || out[9] < 89 {
		t.Fatalf("full-record span wrong: %v", out)
	}

	// Interpolation: half-sample positions land between neighbours.
	out = Window(sig, 100, 10, -1, true, 20, 0.5)
	// pos = 45 + x*0.5 → out[1] interpolates sig[45..46] at 45.5 → 45.5 → 46 or 45
	if out[1] < 45 || out[1] > 46 {
		t.Fatalf("interp out[1] = %d, want ≈45.5", out[1])
	}
}

// TRLC-LINKS: REQ-SDS-163
func TestWindowNoEndGaps(t *testing.T) {
	// The record IS the window (WinCols == Valid) with the edge off-centre:
	// the window must clamp into the record and fill every column with real
	// data (no -1 end gaps), rather than run off the near end.
	sig := make([]uint8, 2048)
	for i := range sig {
		sig[i] = uint8(i % 256)
	}
	// edge at 935 (off-centre) — previously produced ~89 leading gaps.
	// Allow at most a single fractional overshoot at the extreme right.
	out := Window(sig, 2048, 2048, 935, false, 800, 0.5)
	gaps := 0
	for _, v := range out {
		if v < 0 {
			gaps++
		}
	}
	if gaps > 1 {
		t.Fatalf("%d end-gap columns (edge off-centre must clamp into the record)", gaps)
	}
}

// TRLC-LINKS: REQ-SDS-163
func TestWindowRailExtendCentres(t *testing.T) {
	// Repeat-rail: an edge near a record END (no crossing near the middle, the
	// sub-period case) must still land at posFrac. With WinCols==Valid the old
	// left-clamp shoved it off-centre; repeat-rail keeps it centred by extending
	// the rail off-record instead of clamping the window.
	sig := make([]uint8, 2048)
	for i := range sig {
		if i >= 935 { // a single rising step at 935 (the only crossing)
			sig[i] = 255
		}
	}
	out := Window(sig, 2048, 2048, 935, false, 800, 0.5)
	// Find the rising crossing in the rendered window.
	cross := -1
	for x := 1; x < len(out); x++ {
		if out[x-1] < 128 && out[x] >= 128 {
			cross = x
			break
		}
	}
	if cross < 0 {
		t.Fatal("edge vanished from the rendered window")
	}
	// posFrac 0.5 → the edge must sit at ~col 400 of 800 (dead centre), NOT at
	// the clamped ~col 365 the old code produced.
	if cross < 396 || cross > 404 {
		t.Fatalf("edge at col %d of 800 (frac %.3f), want ≈400/0.500", cross, float64(cross)/800)
	}
	for _, v := range out {
		if v < 0 {
			t.Fatal("repeat-rail must never gap (-1)")
		}
	}
}

// TRLC-LINKS: REQ-SDS-163
func BenchmarkWindowFrame(b *testing.B) {
	sig := make([]uint8, 5000)
	for i := range sig {
		sig[i] = uint8(i)
	}
	for b.Loop() {
		Window(sig, 5000, 5000, 2500, false, 800, 0.5)
		Window(sig, 5000, 1000, 2500, true, 800, 0.5)
	}
}

// TRLC-LINKS: REQ-SDS-163
func TestResampleEnv(t *testing.T) {
	v := make([]uint8, 800)
	for i := range v {
		v[i] = uint8(i % 256)
	}
	out := ResampleEnv(v, 800, 400)
	if len(out) != 400 || out[0] != 0 || out[399] != int16(v[399*800/400]) {
		t.Fatalf("resample: len=%d out[399]=%d", len(out), out[399])
	}
}

// A frame with no crossing shows the screen centred on the record (lcd
// traceWindow): at 10 % no column past the record's end repeats its last
// sample.
// TRLC-LINKS: REQ-SDS-163
func TestWindowWithoutAnEdge(t *testing.T) {
	sig := make([]uint8, 1000)
	for i := range sig {
		sig[i] = uint8(i / 4)
	}
	out := Window(sig, 1000, 1000, -1, false, 100, 0.1)
	if out[0] > 2 || out[99] < 245 {
		t.Fatalf("columns from %d to %d, want the record's 0 to 249", out[0], out[99])
	}
}

// A roll view's held samples reach the page as ranges, which it draws dimmed.
// TRLC-LINKS: REQ-SDS-071
func TestHeldRanges(t *testing.T) {
	held := []bool{false, true, true, false, false, true, true, true}
	if got := HeldRanges(held, 8); !reflect.DeepEqual(got, [][2]int{{1, 3}, {5, 8}}) {
		t.Fatalf("ranges %v", got)
	}
	if got := HeldRanges(held, 6); !reflect.DeepEqual(got, [][2]int{{1, 3}, {5, 6}}) {
		t.Fatalf("ranges of the first 6 %v", got)
	}
	if got := HeldRanges(nil, 8); got != nil {
		t.Fatalf("no mask: %v", got)
	}
	testenv.NeedNode(t)
	if out, err := nodeRun(t, "held_split.test.cjs"); err != nil {
		t.Fatalf("held split: %v\n%s", err, out)
	}
}
