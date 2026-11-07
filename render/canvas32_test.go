// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render_test

import (
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/render/rendertest"
)

// A 32-bit canvas shows the scenes as the RGB565 one does, each pixel in
// full precision: packed to RGB565 they agree, but where a colour is worked
// out from others (the eye's shades, dimmed held stretches, the SOUND
// trace's persistence), which a 32-bit canvas works out at 8 bits.
// TRLC-LINKS: REQ-DSO-021
func TestImage32MatchesImage565(t *testing.T) {
	shaded := map[string]bool{"ws_bus_eye": true, "ws_sound_mod": true}
	m := &render.Metrics800x480
	for _, s := range rendertest.Scenes() {
		t.Run(s.Name, func(t *testing.T) {
			a := render.NewImage565(m)
			s.Draw(a)
			b := render.NewImage32(m, hal.XRGB8888)
			s = scene(s.Name)
			s.Draw(b)
			diff := 0
			for y := range m.H {
				for x := range m.W {
					if render.At565(a, x, y) != render.Pack565(b.At(x, y)) {
						diff++
					}
				}
			}
			if diff != 0 && !shaded[s.Name] {
				t.Fatalf("%d pixels differ", diff)
			}
		})
	}
}

// scene is a fresh copy of the named scene (a spectrogram holds state).
func scene(name string) rendertest.Scene {
	for _, s := range rendertest.Scenes() {
		if s.Name == name {
			return s
		}
	}
	panic("no scene " + name)
}

// Every scene paints every pixel at every screen's metrics: a scene that
// starts on a colour no frame uses leaves none of it.
// TRLC-LINKS: REQ-DSO-021
func TestScenesPaintEveryScreen(t *testing.T) {
	fill := render.RGB(255, 0, 255)
	for _, m := range []*render.Metrics{&render.Metrics800x480, &render.Metrics1024x600, &render.Metrics1280x800} {
		for _, s := range append(rendertest.Scenes(), rendertest.FourChanScenes()...) {
			if !s.Prefill {
				continue
			}
			sf := render.NewImage32(m, hal.XRGB8888)
			s.Draw(sf)
			for y := range m.H {
				for x := range m.W {
					if sf.At(x, y) == fill {
						t.Fatalf("%dx%d %s: pixel (%d,%d) not painted", m.W, m.H, s.Name, x, y)
					}
				}
			}
		}
	}
}
