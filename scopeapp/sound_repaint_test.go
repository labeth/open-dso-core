package scopeapp

import (
	"testing"

	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// The SOUND view repaints when its pane changes (a new tuner reading, a
// meter, a cycle) and not otherwise: it is not a time-evolving view that
// bypasses the repaint cache, whose redraws took a fifth of the core and
// ran Listen dry. The Bode sweep and the spectrogram still are.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-273
func TestSoundViewRepaintsOnlyOnChange(t *testing.T) {
	pane := func(cents float64) *wsview.SoundPane {
		return &wsview.SoundPane{
			HasPitch: true, Note: "A4", Cents: cents, Hz: "440.00 Hz", Strip: []float32{1, 2},
			Meter: [2]wsview.SoundMeter{{On: true}}, Traces: [][]float32{{0, 1, 0}},
		}
	}
	h := render.HUD{ViewMode: soundView, UI: wsview.View{Sound: pane(3)}}
	if forceRepaint(h) {
		t.Fatal("the SOUND view bypasses the repaint cache")
	}
	a := renderSigOf(nil, h, false)
	if h.UI.Sound = pane(3); renderSigOf(nil, h, false) != a {
		t.Fatal("the same SOUND pane rebuilt changed the render signature")
	}
	if h.UI.Sound = pane(4); renderSigOf(nil, h, false) == a {
		t.Fatal("a new tuner reading left the render signature unchanged")
	}
	for _, v := range []int{3, 4} {
		if !forceRepaint(render.HUD{ViewMode: v}) {
			t.Fatalf("view %d (a sweep or a waterfall) uses the repaint cache", v)
		}
	}
}

// A key that changes only a value in a sheet, a pane's text, the rail label
// or a notice repaints a held display (TRIGGER Type Pulse->Slope while
// stopped must not leave "Pulse" on the LCD); the same snapshot rebuilt (new
// pointers, the same content) does not.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-211
func TestRenderSigSeesUIChanges(t *testing.T) {
	view := func(typ string) wsview.View {
		return wsview.View{
			Layout: wsview.Layout{MenuW: 116, Sheet: true},
			Sheet: &wsview.Sheet{
				WSName: "SCOPE", Rows: []wsview.Row{{
					Kind: wsview.Enum, Label: "Type",
					Opts: []wsview.Opt{{Text: "Pulse", Sel: typ == "Pulse"}, {Text: "Slope", Sel: typ == "Slope"}},
				}},
				Sub: &wsview.Sub{Title: "x"},
			},
			Keys: &wsview.Keys{Title: "TYPE"},
		}
	}
	h := render.HUD{UI: view("Pulse")}
	a := renderSigOf(nil, h, false)
	if h.UI = view("Pulse"); renderSigOf(nil, h, false) != a {
		t.Fatal("the same UI rebuilt changed the render signature")
	}
	if h.UI = view("Slope"); renderSigOf(nil, h, false) == a {
		t.Fatal("a sheet value change left the render signature unchanged")
	}
	h.UI = view("Pulse")
	h.UI.RailNote = &wsview.Note{Head: "F1 SCOPE"}
	if renderSigOf(nil, h, false) == a {
		t.Fatal("the rail label left the render signature unchanged")
	}
	h.UI = view("Pulse")
	h.Notice = "SAVED SDS0001.png"
	if renderSigOf(nil, h, false) == a {
		t.Fatal("a PRINT notice left the render signature unchanged")
	}
	h.Notice = ""
	h.Remote = "REMOTE  TRIG level 1.25V"
	if renderSigOf(nil, h, false) == a {
		t.Fatal("a REMOTE note left the render signature unchanged")
	}
}
