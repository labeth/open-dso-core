// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

// The internals the external tests and benchmarks drive with the golden
// fixtures of rendertest.
var (
	DrawMeasPanel     = drawMeasPanel
	FullPlot          = fullPlot
	MathCodes         = mathCodes
	DrawSoundPane     = drawSoundPane
	DrawSoundTrace    = drawSoundTrace
	PlotOf            = plotOf
	DrawSuperresTrace = drawSuperresTrace
	SRResampleArray   = srResampleArray
	ColC1             = colC1
	ColOK             = colOK
	At565             = at565
	Pack565           = pack565
)

// The saver box's size on the 800×480 screen.
const (
	SaverW    = 240
	SaverH    = 144
	SaverBoxH = SaverH + saverLinesH
)
