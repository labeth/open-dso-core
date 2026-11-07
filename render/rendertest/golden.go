// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package rendertest

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// MaxDiffPixels is how many pixels a render may differ from its checked-in
// golden. Benign cross-platform float rounding moves a trace edge by a pixel
// at a handful of columns; a real regression (colour, layout, missing
// element) flips hundreds to thousands.
const MaxDiffPixels = 64

// Diff counts the pixels whose colour differs between two PNG images, and
// reports the first.
func Diff(got, want []byte) (int, image.Point, error) {
	a, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		return 0, image.Point{}, fmt.Errorf("render undecodable: %w", err)
	}
	b, err := png.Decode(bytes.NewReader(want))
	if err != nil {
		return 0, image.Point{}, fmt.Errorf("golden undecodable: %w", err)
	}
	if a.Bounds() != b.Bounds() {
		return 0, image.Point{}, fmt.Errorf("render bounds %v, golden %v", a.Bounds(), b.Bounds())
	}
	n, first := 0, image.Pt(-1, -1)
	r := b.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			if ar != br || ag != bg || ab != bb {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, first, nil
}

// Check compares a render (PNG) with dir/name.png, allowing allowance
// differing pixels; update rewrites the golden instead.
func Check(t testing.TB, dir, name string, got []byte, allowance int, update bool) {
	t.Helper()
	path := filepath.Join(dir, name+".png")
	if update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // a golden image, world-readable like the source
			t.Fatal(err)
		}
		t.Logf("golden rewritten: %s", path)
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // the test's own golden directory
	if err != nil {
		t.Fatalf("missing golden %s (%v): regenerate with -update-golden", path, err)
	}
	n, first, err := Diff(got, want)
	if err != nil {
		t.Fatalf("golden %s: %v", name, err)
	}
	if n > allowance {
		rej := filepath.Join(t.TempDir(), name+".got.png")
		if err := os.WriteFile(rej, got, 0o644); err == nil { //nolint:gosec // a test artefact
			t.Logf("rendered image saved to %s", rej)
		}
		t.Fatalf("golden %s: %d pixels differ (allowance %d), first at %v; if the change is intended, regenerate with -update-golden", name, n, allowance, first)
	}
	if n > 0 {
		t.Logf("golden %s: %d pixels differ (within the %d-pixel allowance)", name, n, allowance)
	}
}
