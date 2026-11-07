// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render_test

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

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

// TRLC-LINKS: REQ-SDS-021, REQ-SDS-211
func TestGolden(t *testing.T) {
	for _, s := range rendertest.Scenes() {
		t.Run(s.Name, func(t *testing.T) {
			sf := render.NewImage565(&render.Metrics800x480)
			s.Draw(sf)
			goldenCheck(t, s.Name, render.EncodePNG(sf))
		})
	}
}

// goldenCheck compares one render with its golden, as the flags say.
func goldenCheck(t *testing.T, name string, got []byte) {
	t.Helper()
	if *goldenSave != "" {
		if err := os.WriteFile(filepath.Join(*goldenSave, name+".png"), got, 0o644); err != nil { //nolint:gosec // a test artefact
			t.Fatal(err)
		}
	}
	if *goldenExact != "" {
		rendertest.Check(t, *goldenExact, name, got, 0, false)
		return
	}
	rendertest.Check(t, filepath.Join("testdata", "golden"), name, got, rendertest.MaxDiffPixels, *updateGolden)
}
