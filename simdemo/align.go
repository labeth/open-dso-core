package simdemo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/labeth/open-dso-core/hal/keytable"
	"github.com/labeth/open-dso-core/scopeapp"
)

// Differences are the fields where the demo's composition (Compose) may
// differ from a scope's device main's (scopeapp.Describe, scopeapp.Diff),
// each with why it must; TestCompositionsAgree fails on any other, and on
// a reason no scope's composition needs any more.
var Differences = map[string]string{
	// Every scope's.
	"power_on":         "a device powers on in its own state (its owner's, or its SCPI dialect's defaults); the demo powers on in the bench's setup (the demo file's start), so its first picture shows the generator's signals; DEFAULT SETUP is the product's own in both",
	"ui.tdiv_at_start": "the UI model starts at the power-on timebase (power_on)",
	"parts.check":      "a device's state check is its own (its owner's, or its dialect's ranges, which SCPI can ask beyond the panel's ladders and the web's lists); the demo's refuses a timebase that is not positive, and the simulated board refuses at commit",
	"parts.scpi":       "SCPI is served on LAN transports a browser tab cannot listen on, in the scope's dialect, which is the scope's code; the demo's one module carries none (ADR-DEMO-ONE-MODULE)",

	// A scope whose board runs on the core's HAL cycle with parts of its
	// own beside it: the demo composes the core's over the simulated board.
	"parts.ui":                 ownUI,
	"parts.display.url":        "the scope's LCD shows the unit's own address on its busy indicator; the demo's page has none",
	"parts.display.canvas":     "the scope's headless pictures are drawn into its LCD's own back buffer; the demo's at the profile's display",
	"parts.display.open":       "the scope's own LCD driver opens a framebuffer the board does not carry, with its afterglow layer and page-flipped copy; the demo's display is the simulated board's",
	"parts.display.engine":     ownDisplay,
	"display":                  ownDisplay,
	"parts.web":                ownWeb,
	"web":                      ownWeb,
	"parts.settings.analog":    "the scope's settings file keeps its front end's own detents and offsets as it always kept them; the demo's keeps the model's channels",
	"parts.settings.extension": "the scope's settings file keeps the trigger level as its comparator's DAC code (level_code); the demo's keeps the level in volts",
	"parts.settings.slots":     "the scope's own setup memory names its slots by file name; the simulated board's store keys them by number",
	"parts.settings.controls":  "the scope's setup is collected through the HAL scope's engine its UI model runs over, so its file keeps the engine's parts (record depth, frequency-response channels, roll stream) as it always did; the demo's through the instrument state",
	"ui.streams":               "the scope's board streams decoded buses gap-free in its own sessions; the simulated board has no streamer, so the core's UI model streams none",
	"ui.records":               "the scope's own record memory plans each timebase; the simulated board plans with the profile's rates and the demo file's record depth and screens (a board difference, compared by the sessions)",
}

// ownUI is why a scope builds its own UI model over the HAL scope's
// engine.
const ownUI = "the scope's UI model runs over the HAL scope's engine with its own front end (its detents, its offset DACs), its levels (the comparator's DAC code) and its hardware facts; the demo's is the core's over the simulated board"

// ownDisplay is why a scope's display asks its own acquisition side.
const ownDisplay = "the scope's display ticks at its board's render knob (the debug tuning's) and counts the pictures it puts up for the status, over the cycle's pace and quiet lock; the demo's ticks at the profile's display rate"

// ownWeb is why a scope serves its own web scope.
const ownWeb = "the scope serves its own web scope over the HAL scope's engine (its own routes, its v1 page's DAC codes, its board's counters); the demo serves the core's over the simulated board"

// CompositionFixture is a scope's device composition as the core keeps it
// for the demo's alignment test: the description its device main's
// Options give (demotest.Composition), and the profile and demo file the
// demo's composition is built from.
type CompositionFixture struct {
	Scope   string               `json:"scope"`
	Profile string               `json:"profile"`
	Demo    string               `json:"demo"`
	Device  scopeapp.Composition `json:"device"`
}

// CompositionsDir is where the core keeps the scopes' device compositions.
// TRLC-LINKS: REQ-DSO-295
func CompositionsDir() string {
	return filepath.Join(coreDir(), "simdemo", "testdata", "compositions")
}

// panelLayout is the page's front panel: items at grid cells, each a key
// (with its lamps), a knob (with its push key) or a section label, by the
// panel's own control names.
type panelLayout struct {
	Softkeys []panelItem `json:"softkeys"`
	Items    []panelItem `json:"items"`
}

// panelItem is one control of the page's panel.
type panelItem struct {
	Key  string `json:"key"`
	Knob string `json:"knob"`
	Push string `json:"push"`
	Sect string `json:"sect"`
}

// CheckPanel reports where the page's front panel and the board's key
// table disagree: a control the table does not have, one the page does
// not place, or one placed twice. A demo file without a key table is
// checked against the profile's names by the simulated board alone.
// TRLC-LINKS: REQ-DSO-295
func (d Demo) CheckPanel() error {
	t := d.KeyTable
	if t.Empty() {
		return nil
	}
	var p panelLayout
	if len(d.Panel) > 0 {
		if err := json.Unmarshal(d.Panel, &p); err != nil {
			return fmt.Errorf("panel: %w", err)
		}
	}
	placed := map[string]int{}
	var errs []error
	for _, it := range slices.Concat(p.Softkeys, p.Items) {
		if it.Key != "" {
			placed["key "+it.Key]++
			if _, ok := t.Key(it.Key); !ok {
				errs = append(errs, fmt.Errorf("panel: key %q is not on the board's panel", it.Key))
			}
		}
		if it.Push != "" {
			placed["key "+it.Push]++
			if _, ok := t.Key(it.Push); !ok {
				errs = append(errs, fmt.Errorf("panel: knob %q's push %q is not on the board's panel", it.Knob, it.Push))
			}
		}
		if it.Knob != "" {
			placed["knob "+it.Knob]++
			if _, ok := t.Knob(it.Knob); !ok {
				errs = append(errs, fmt.Errorf("panel: knob %q is not on the board's panel", it.Knob))
			}
		}
	}
	for _, k := range t.Keys {
		switch placed["key "+k.Panel] {
		case 0:
			errs = append(errs, fmt.Errorf("panel: the board's key %q is not on the page", k.Panel))
		case 1:
		default:
			errs = append(errs, fmt.Errorf("panel: key %q is on the page %d times", k.Panel, placed["key "+k.Panel]))
		}
	}
	for _, k := range t.Knobs {
		if placed["knob "+k.Panel] != 1 {
			errs = append(errs, fmt.Errorf("panel: the board's knob %q is on the page %d times, want once", k.Panel, placed["knob "+k.Panel]))
		}
	}
	return errors.Join(errs...)
}

// keyTableField is the demo file's key table's field.
const keyTableField = `"keytable":`

// WithKeyTable is demo file raw with its key table set to t, the rest of
// the file as it is: the table is the file's last field, one control a
// line.
// TRLC-LINKS: REQ-DSO-295
func WithKeyTable(raw []byte, t keytable.Table) ([]byte, error) {
	var b strings.Builder
	b.WriteString(keyTableField + " {\n    \"keys\": [\n")
	for i, k := range t.Keys {
		line, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.WriteString("      " + string(line) + comma(i, len(t.Keys)) + "\n")
	}
	b.WriteString("    ],\n    \"knobs\": [\n")
	for i, k := range t.Knobs {
		line, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		b.WriteString("      " + string(line) + comma(i, len(t.Knobs)) + "\n")
	}
	b.WriteString("    ]\n  }\n}\n")
	body := bytes.TrimRight(raw, " \n\t")
	if i := bytes.Index(body, []byte(",\n  "+keyTableField)); i >= 0 {
		body = body[:i]
	} else {
		body = bytes.TrimSuffix(body, []byte("}"))
		body = bytes.TrimRight(body, " \n\t")
	}
	out := append(append([]byte{}, body...), []byte(",\n  "+b.String())...)
	var check Demo
	if err := json.Unmarshal(out, &check); err != nil {
		return nil, fmt.Errorf("the demo file with its key table: %w", err)
	}
	return out, nil
}

// comma is the separator after element i of n.
// TRLC-LINKS: REQ-DSO-295
func comma(i, n int) string {
	if i < n-1 {
		return ","
	}
	return ""
}
