// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package rendertest

import (
	"github.com/labeth/open-dso-core/uimodel/wsview"
)

// The touch-first layout's golden scenes: the workspace scenes laid out as
// the panel lays them out for a touch screen without F-keys (no rail, no
// F-key column, the readout bar and the touch bar, the sheet as a panel).

// touchActions is the touch bar's right end with the scope running.
// TRLC-LINKS: REQ-DSO-299
func touchActions(menu, tools bool) []wsview.Btn {
	return []wsview.Btn{
		{ID: "act:auto", Label: "AUTO"},
		{ID: "act:run", Label: "RUN", Col: wsview.Run},
		{ID: "act:single", Label: "SINGLE"},
		{ID: "act:tools", Label: "TOOLS", Sel: tools, Dim: menu},
		{ID: "act:menu", Label: "MENU", Sel: menu},
	}
}

// touchViews is a segmented control of labels, sel selected.
// TRLC-LINKS: REQ-DSO-299
func touchViews(sel int, labels ...string) []wsview.Btn {
	out := make([]wsview.Btn, 0, len(labels))
	for i, l := range labels {
		out = append(out, wsview.Btn{ID: "view:" + string(rune('0'+i)), Label: l, Sel: i == sel})
	}
	return out
}

// toTouch lays scene s out for touch with bar b: the rail, its note and the
// F-key column go, the plot takes the full width.
// TRLC-LINKS: REQ-DSO-299
func toTouch(s Scene, name string, b *wsview.TouchBar) Scene {
	v := &s.HUD.UI
	v.Layout.Touch, v.Layout.MenuW = true, 0
	v.Rail, v.RailNote, v.Keys = false, nil, nil
	v.Bar = b
	s.Name = name
	return s
}

// TouchScenes are the touch-first layout's golden scenes: every workspace,
// a sheet panel with a chooser, the byte picker's choices in the bar, the
// match editor, the knobs' jobs, and a selection box over a zoomed view.
// TRLC-LINKS: REQ-DSO-299
func TouchScenes() []Scene {
	scope := toTouch(sceneScopeRail(), "touch_scope", &wsview.TouchBar{Views: touchViews(0, "Y-T", "X-Y"), Actions: touchActions(false, false)})
	bus := toTouch(sceneBusPackets(), "touch_bus_packets", &wsview.TouchBar{Views: touchViews(0, "Packets", "Eye", "Stream", "Visual"), Actions: touchActions(false, false)})
	eye := toTouch(sceneBusEye(), "touch_bus_eye", &wsview.TouchBar{Views: touchViews(1, "Packets", "Eye", "Stream", "Visual"), Actions: touchActions(false, false)})
	spec := toTouch(sceneSpectrumPeaks(), "touch_spectrum", &wsview.TouchBar{Views: touchViews(0, "FFT", "Spectro", "Bode", "Sound"), Actions: touchActions(false, false)})
	meas := toTouch(sceneMeasureStats(), "touch_measure", &wsview.TouchBar{Views: touchViews(0, "Stats", "All values"), Actions: touchActions(false, false)})
	cmp := toTouch(sceneCompare(), "touch_compare", &wsview.TouchBar{Views: []wsview.Btn{{ID: "view:0", Label: "Cancel stack", Sel: true}}, Actions: touchActions(false, false)})
	sound := toTouch(sceneSoundModulation(), "touch_sound", &wsview.TouchBar{Views: touchViews(3, "FFT", "Spectro", "Bode", "Sound"), Actions: touchActions(false, false)})

	sheet := toTouch(sceneScopeRail(), "touch_sheet", &wsview.TouchBar{Views: touchViews(0, "Y-T", "X-Y"), Actions: touchActions(true, false)})
	sh := Sheet()
	for i := range sh.Tabs {
		sh.Tabs[i].Own = i < 5
	}
	sheet.HUD.UI.Sheet = sh

	chooser := toTouch(sceneScopeRail(), "touch_sheet_chooser", &wsview.TouchBar{Views: touchViews(0, "Y-T", "X-Y"), Actions: touchActions(true, false)})
	sh2 := Sheet()
	sh2.Choose = &wsview.Chooser{Title: "Level", Opts: []wsview.Opt{{Text: "0 V"}, {Text: "50 %"}, {Text: "Default"}}}
	chooser.HUD.UI.Sheet = sh2

	picker := toTouch(sceneBusPicker(), "touch_bus_picker", &wsview.TouchBar{Actions: touchActions(false, false), Keys: []wsview.Btn{
		{ID: "key:0", Label: "Trig on 1 48"},
		{ID: "key:1", Label: "Trig on 2 48 69"},
		{ID: "key:2", Label: "Trig on 3"},
		{ID: "key:3", Label: "Trig on 4"},
		{ID: "key:4", Label: "Cancel pick"},
		{ID: "pick:-1", Label: "< byte"},
		{ID: "pick:1", Label: "byte >"},
	}})
	match := toTouch(sceneMatchEditor(), "touch_match_editor", &wsview.TouchBar{Keys: []wsview.Btn{
		{ID: "key:0", Label: "Keys Hex"},
		{ID: "key:1", Label: "Type A"},
		{ID: "key:2", Label: "Delete <"},
		{ID: "key:3", Label: "OK"},
		{ID: "key:4", Label: "Cancel"},
		{ID: "mcur:-1", Label: "< cursor"},
		{ID: "mcur:1", Label: "cursor >"},
		{ID: "mclear", Label: "Clear all"},
	}})
	match.HUD.UI.Sheet.Tabs = []wsview.Tab{{Name: "BUS", Own: true, Active: true}}

	tools := toTouch(sceneBusPackets(), "touch_tools", &wsview.TouchBar{Actions: touchActions(false, true), Keys: []wsview.Btn{
		{ID: "tool:adj-", Label: "< Adjust"},
		{ID: "tool:adj+", Label: "Adjust >"},
		{ID: "tool:adjpush", Label: "Adjust push"},
		{ID: "tool:pos-", Label: "< Position"},
		{ID: "tool:pos+", Label: "Position >"},
		{ID: "tool:pospush", Label: "Position push"},
		{ID: "tool:zoom", Label: "Zoom on/off"},
		{ID: "tool:done", Label: "Done", Sel: true},
	}})

	box := toTouch(sceneScopeRail(), "touch_box_zoomed", &wsview.TouchBar{Views: touchViews(0, "Y-T", "X-Y"), Actions: touchActions(false, false), Zoom: "Zoom 5x"})
	box.HUD.Zoom = 5
	box.HUD.UI.Box = &wsview.Box{X0: 300, Y0: 120, X1: 520, Y1: 380}

	return []Scene{scope, bus, eye, spec, meas, cmp, sound, sheet, chooser, picker, match, tools, box}
}
