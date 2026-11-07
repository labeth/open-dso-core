// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render_test

import (
	"flag"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/render/rendertest"
)

// Golden-image harness: each rendertest scene is rendered into a canvas,
// PNG-encoded (the same EncodePNG the web's screen view uses) and compared
// pixel by pixel with testdata/golden/. The renderer is pure Go over a fixed
// bitmap font, so renders are deterministic up to cross-platform float
// rounding at trace edges.
//
// Regenerating goldens after an intended render change (commit the PNGs with
// the change that caused them):
//
//	go test ./render -run Golden -update-golden
var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/golden/*.png from the current render")

// A refactor that must not move a pixel saves this platform's renders before
// it and compares against them exactly after it:
//
//	go test ./render -run Golden -golden-save /tmp/before
//	go test ./render -run Golden -golden-exact /tmp/before
var (
	goldenSave  = flag.String("golden-save", "", "also write each golden render into this directory")
	goldenExact = flag.String("golden-exact", "", "compare each golden render exactly against this directory's")
)

// TRLC-LINKS: REQ-DSO-021, REQ-DSO-211
func TestGolden(t *testing.T) {
	for _, s := range rendertest.Scenes() {
		t.Run(s.Name, func(t *testing.T) {
			sf := render.NewImage565(&render.Metrics800x480)
			s.Draw(sf)
			goldenCheck(t, s.Name, render.EncodePNG(sf))
		})
	}
}

// The same scenes on a 1024×600 XRGB8888 screen: the right column, the
// strips and the readouts follow the screen, the font and the panes keep
// their size.
// TRLC-LINKS: REQ-DSO-021, REQ-DSO-211
func TestGolden1024x600(t *testing.T) {
	for _, s := range rendertest.Scenes() {
		t.Run(s.Name, func(t *testing.T) {
			sf := render.NewImage32(&render.Metrics1024x600, hal.XRGB8888)
			s.Draw(sf)
			goldenCheck(t, "1024x600/"+s.Name, render.EncodePNG(sf))
		})
	}
}

// Four channels of 12-bit codes on a 1024×600 ARGB8888 screen: drawn by
// the CPU, and as the UI over a hardware waveform renderer whose 1000×480
// plane shows through a transparent area (ADR-HAL-UI-OVER-HW-RENDERER).
// TRLC-LINKS: REQ-DSO-021
func TestGolden4Ch12Bit(t *testing.T) {
	for _, s := range rendertest.FourChanScenes() {
		t.Run(s.Name, func(t *testing.T) {
			sf := render.NewImage32(&render.Metrics1024x600, hal.ARGB8888)
			s.Draw(sf)
			goldenCheck(t, "1024x600argb/"+s.Name, render.EncodePNG(sf))
		})
		t.Run(s.Name+"_over_hw", func(t *testing.T) {
			sf := render.NewImage32(&render.Metrics1024x600, hal.ARGB8888)
			sf.SetUnder(hwPlane)
			s.Draw(sf)
			clear := 0 // the UI drawn over the plane (markers, readouts, the rail) may cover some of it
			for y := hwPlane.Min.Y; y < hwPlane.Max.Y; y++ {
				for x := hwPlane.Min.X; x < hwPlane.Max.X; x++ {
					if sf.At(x, y) == render.Transparent {
						clear++
					}
				}
			}
			if area := hwPlane.Dx() * hwPlane.Dy(); clear < area*3/4 {
				t.Fatalf("%d of the waveform plane's %d pixels transparent", clear, area)
			}
			if sf.At(0, 0) == render.Transparent || sf.At(hwPlane.Max.X, hwPlane.Max.Y) == render.Transparent {
				t.Fatal("the UI outside the plane is transparent")
			}
			goldenCheck(t, "1024x600argb/"+s.Name+"_over_hw", render.EncodePNG(sf))
		})
	}
}

// hwPlane is a 1000×480 waveform plane under a 1024×600 screen, as the
// SDS800X HD's PL composes it under fb0.
var hwPlane = image.Rect(12, 60, 1012, 540)

// goldenCheck compares one render with its golden, as the flags say; name
// may hold a subdirectory.
func goldenCheck(t *testing.T, name string, got []byte) {
	t.Helper()
	if *goldenSave != "" {
		path := filepath.Join(*goldenSave, name+".png")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir, base := filepath.Split(name)
	if *goldenExact != "" {
		rendertest.Check(t, filepath.Join(*goldenExact, dir), base, got, 0, false)
		return
	}
	rendertest.Check(t, filepath.Join("testdata", "golden", dir), base, got, rendertest.MaxDiffPixels, *updateGolden)
}
