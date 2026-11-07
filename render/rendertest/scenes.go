// Package rendertest holds the display's golden scenes: fixed frames and HUD
// snapshots covering every view and workspace, rendered by the core's golden
// tests at each screen's metrics and by a board's own golden test through
// its display path.
// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER
package rendertest

import (
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
)

// Scene is one golden picture: a frame (nil when the view draws from the
// HUD alone) and the HUD it is drawn with.
type Scene struct {
	Name  string
	Frame *sample.Frame
	HUD   render.HUD
	// Prefill starts the canvas on a colour no frame uses, as the display's
	// back buffer holds the last frame: Render must paint every pixel.
	Prefill bool
}

// Draw renders the scene into sf as a live frame.
// TRLC-LINKS: REQ-DSO-021
func (s Scene) Draw(sf render.Canvas) {
	if s.Prefill {
		sf.Fill(render.RGB(255, 0, 255))
	}
	render.Render(sf, s.Frame, s.HUD, true)
}

// Scenes builds the golden scenes, fresh each call (a HUD's spectrogram
// holds state).
// TRLC-LINKS: REQ-DSO-021
func Scenes() []Scene {
	return []Scene{
		sceneYTDual(),
		sceneYTDecodeUART(),
		sceneFFTPeaks(),
		sceneXY(),
		sceneMeasurePanel(),
		sceneEnvelope(),
		sceneBode(),
		sceneSpectrogram(),
		sceneCursorsTime(),
		sceneCursorsVolts(),
		sceneScopeRail(),
		sceneBusPackets(),
		sceneBusPicker(),
		sceneBusEye(),
		sceneBusStream(),
		sceneSpectrumPeaks(),
		sceneMeasureStats(),
		sceneCompare(),
		sceneSheet(),
		sceneSheetPicker(),
		sceneMatchEditor(),
		sceneSoundModulation(),
		sceneSoundEnvelope(),
		sceneSoundTake(),
		sceneSoundHarmonics(),
		sceneSuperresReview(),
		sceneSuperresReviewFFT(),
	}
}
