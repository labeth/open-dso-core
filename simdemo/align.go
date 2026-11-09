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

	// A scope that runs its own acquisition owner until it is HAL terms
	// throughout: the demo runs the core's HAL loop over the simulated
	// board, with the core's UI model, display, web and settings over it.
	"parts.owner":              ownOwner,
	"parts.ui":                 ownOwner,
	"parts.display.engine":     ownOwner,
	"parts.display.hud":        ownOwner,
	"parts.display.onframe":    ownOwner,
	"parts.display.url":        ownOwner,
	"parts.display.canvas":     ownOwner,
	"parts.display.open":       "the scope's own LCD driver opens a framebuffer the board does not carry; the demo's display is the simulated board's",
	"parts.web":                ownOwner,
	"parts.settings.analog":    ownOwner,
	"parts.settings.extension": ownOwner,
	"parts.settings.slots":     "the scope's own setup memory names its slots by file name; the simulated board's store keys them by number",
	"display":                  ownOwner,
	"web":                      ownOwner,
	"ui.streams":               "the scope's own engine streams decoded buses gap-free; the simulated board has no streamer, so the core's UI model streams none",
	"ui.records":               "the scope's own record memory plans each timebase; the simulated board plans with the profile's rates and the demo file's record depth and screens (a board difference, compared by the sessions)",
}

// ownOwner is the reason for the parts a scope with its own acquisition
// owner builds itself.
const ownOwner = "the scope runs its own acquisition owner over its bus until it is HAL terms throughout; the demo runs the core's HAL loop, UI model, display, web and settings over the simulated board"

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
