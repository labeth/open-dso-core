// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/scopeweb"
	"github.com/labeth/open-dso-core/uimodel/panel"
)

// Composition describes what Run composes from a board and Options, as
// data: where each part of the product comes from and the values it is
// built with. Two compositions of one scope (its device main and its demo)
// are compared field by field (Diff), so a part one adds, leaves out or
// builds otherwise shows.
type Composition struct {
	// Parts names where each part comes from: "core" (the core's own over
	// the board), "board" (the board's facet), "scope" (the scope's own
	// builder) or "none".
	Parts map[string]string `json:"parts"`
	// PowerOn is the instrument's power-on state on the HAL path (nil with
	// an Owner, which keeps its own).
	PowerOn *instrument.State `json:"power_on,omitempty"`
	// UI is what the UI model plans with.
	UI *UIFacts `json:"ui,omitempty"`
	// Display is the display loop's cadence over the board's engine.
	Display *DisplayFacts `json:"display,omitempty"`
	// Web is the core's web scope's configuration.
	Web *WebFacts `json:"web,omitempty"`
	// Host is what only the host decides (where the web is served, the
	// LAN transports, the build's name and version, the memory limit, the
	// log hooks). It is recorded, never compared.
	Host map[string]string `json:"host"`
}

// UIFacts are the UI model's hardware facts and its timebase ladder.
type UIFacts struct {
	Tdivs       []float64 `json:"tdivs"`
	TdivAtStart float64   `json:"tdiv_at_start"`
	// Records is the record a capture takes at each timebase (every
	// channel on, at the planner's own depth).
	Records []RecordFact `json:"records"`
	// RollMinTdivS is the timebase from which the live view rolls; 0:
	// never.
	RollMinTdivS   float64 `json:"roll_min_tdiv_s"`
	SearchSamples  int     `json:"search_samples"`
	EnvelopePoints int     `json:"envelope_points"`
	// Streams are the decoded streams that run ("protocol/baud": mode).
	Streams map[string]string `json:"streams,omitempty"`
}

// RecordFact is the record of one timebase.
type RecordFact struct {
	TdivS     float64 `json:"tdiv_s"`
	SampleS   float64 `json:"sample_s"`
	DecimLog2 int     `json:"decim_log2"`
}

// DisplayFacts are the display loop's cadence.
type DisplayFacts struct {
	PeriodMS float64 `json:"period_ms"`
	Paced    bool    `json:"paced"` // the board takes the display's pace
}

// WebFacts are the core web scope's name and vertical lists.
type WebFacts struct {
	Name     string    `json:"name"`
	Probes   []float64 `json:"probes"`
	BWLimits []string  `json:"bw_limits"`
}

// streamProbes are the decoded streams a description asks the UI model
// about: the engine's serial protocol numbers at a slow, a common and a
// fast rate.
var streamProbes = struct {
	protos []int
	bauds  []int
}{protos: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, bauds: []int{9600, 115200, 1000000}}

// UIFactsOf are the facts of a UI model built over hw with the timebase
// ladder tdivs, starting at tdiv0.
// TRLC-LINKS: REQ-DSO-295
func UIFactsOf(hw panel.Hardware, tdivs []float64, tdiv0 float64) *UIFacts {
	u := &UIFacts{Tdivs: slices.Clone(tdivs), TdivAtStart: tdiv0, SearchSamples: hw.SearchSamples(), EnvelopePoints: hw.EnvelopePoints()}
	for _, t := range tdivs {
		s, d := hw.Record(t)
		u.Records = append(u.Records, RecordFact{TdivS: t, SampleS: s, DecimLog2: d})
	}
	if r := hw.RollMinTdivS(); !math.IsInf(r, 0) && !math.IsNaN(r) {
		u.RollMinTdivS = r
	}
	for _, p := range streamProbes.protos {
		for _, baud := range streamProbes.bauds {
			if m := hw.StreamMode(p, baud, false); m != "" {
				if u.Streams == nil {
					u.Streams = map[string]string{}
				}
				u.Streams[strconv.Itoa(p)+"/"+strconv.Itoa(baud)] = m
			}
		}
	}
	return u
}

// Describe is the composition Run builds from board b and o, without
// running it: the board is asked only for its profile, its planner and its
// optional parts.
// TRLC-LINKS: REQ-DSO-295
func Describe(b hal.Board, o Options) Composition {
	c := Composition{Parts: map[string]string{}, Host: map[string]string{}}
	part := func(name string, scope bool, otherwise string) {
		c.Parts[name] = otherwise
		if scope {
			c.Parts[name] = "scope"
		}
	}
	p := b.Profile()
	part("owner", o.Owner != nil, "core")
	part("ui", o.UI != nil, "core")
	part("input", o.Input != nil, "board")
	part("display.open", o.Display.Open != nil, "board")
	part("display.engine", o.Display.Engine != nil, "board")
	part("display.hud", o.Display.HUD != nil, "core")
	part("display.onframe", o.Display.OnFrame != nil, "none")
	part("display.url", o.Display.URL != nil, "none")
	part("display.canvas", o.Display.Canvas != nil, "core")
	part("check", o.Check != nil, "none")
	part("scpi", o.SCPI != nil, "none")
	part("web", o.Web != nil, "core")
	part("settings.analog", o.Settings.Analog != nil, "core")
	part("settings.extension", o.Settings.Extension != nil, "core")
	part("settings.slots", o.Settings.SlotKey != nil, "core")
	if o.Listen == "" && o.Serve == nil {
		c.Parts["web"] = "none"
	}

	if o.Owner == nil && o.Defaults != nil {
		st := o.Defaults(p.Channels)
		c.PowerOn = &st
		if o.UI == nil {
			// The facts at a reference state, every channel on at the
			// planner's own depth, so they are the composition's, not
			// the power-on state's.
			ref := st
			ref.Channels = slices.Clone(st.Channels)
			for i := range ref.Channels {
				ref.Channels[i].On = true
			}
			ref.Depth = 0
			m := instrument.New(ref, o.Check)
			s := panel.NewHALScope(m, b, o.Product)
			c.UI = UIFactsOf(s.Hardware(), s.Tdivs(), st.TimeDivS)
		}
	}
	if o.Display.Engine == nil {
		e := boardEngineOf(b)
		c.Display = &DisplayFacts{PeriodMS: float64(e.period) / float64(time.Millisecond), Paced: e.pace != nil}
	}
	if o.Web == nil && c.Parts["web"] != "none" {
		cfg := scopeweb.Config{Board: b, Product: o.webName()}.Resolved()
		c.Web = &WebFacts{Name: cfg.Product, Probes: cfg.Probes, BWLimits: cfg.BWLimits}
	}

	host := func(name string, v any) { c.Host[name] = fmt.Sprint(v) }
	host("listen", o.Listen)
	host("serve", o.Serve != nil)
	host("product", o.Product)
	host("version", o.Version)
	host("memory_limit", o.MemoryLimit)
	host("scpi.transports", fmt.Sprintf("raw=%q telnet=%q vxi11=%v", o.SCPIHost.Raw, o.SCPIHost.Telnet, o.SCPIHost.VXI11))
	host("web.options", len(o.WebOptions))
	host("publish", o.Publish != nil)
	host("mark", o.Mark != nil)
	host("started", o.Started != nil)
	host("on_stop", o.OnStop != nil)
	if o.Describe != nil {
		o.Describe(&c)
	}
	return c
}

// Diff is every field where compositions a and b differ, by its path
// ("parts.owner", "ui.records", "web.probes"), sorted; the host's fields
// are left out.
// TRLC-LINKS: REQ-DSO-295
func Diff(a, b Composition) []string {
	fa, fb := flatten(a), flatten(b)
	var out []string
	for _, k := range slices.Sorted(maps.Keys(fa)) {
		if v, ok := fb[k]; !ok || v != fa[k] {
			out = append(out, k)
		}
	}
	for k := range fb {
		if _, ok := fa[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// flatten is c's compared fields as path to JSON text, one level below
// each section (a whole list or map is one field).
// TRLC-LINKS: REQ-DSO-295
func flatten(c Composition) map[string]string {
	c.Host = nil
	raw, _ := json.Marshal(c) //nolint:errchkjson // a composition is plain data
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	out := map[string]string{}
	for k, v := range top {
		var sub map[string]json.RawMessage
		if json.Unmarshal(v, &sub) != nil {
			out[k] = string(v)
			continue
		}
		for sk, sv := range sub {
			out[k+"."+sk] = string(sv)
		}
	}
	return out
}
