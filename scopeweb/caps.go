// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"encoding/json"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// capsCache gives the document a generation that moves whenever its
// content does (an image switch, a variant, a probe that moves the ladder).
type capsCache struct {
	mu   sync.Mutex
	hash uint64
	gen  uint64
}

// capsGen is the document's generation now.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) capsGen() uint64 { return s.capsDoc().Gen }

// CapsDocument is the capabilities document a scope over a board serves
// (WithCaps): for a scope whose own adapter wraps this one and serves it.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) CapsDocument() wire.Caps { return s.capsDoc() }

// capsDoc is the capabilities document projected from the HAL: the
// profile, the trigger's caps (the current image's), the front end's
// ladders and couplings, the display, the storage and the parts present.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) capsDoc() wire.Caps {
	b := s.cfg.Board
	p := b.Profile()
	st := s.state()
	n := p.Channels
	c := wire.Caps{
		Identity: wire.Identity{Vendor: p.Identity.Vendor, Model: p.Identity.Model, Product: s.cfg.Product, Firmware: s.cfg.Version, Stage: p.Stage},
	}
	if !s.v1Wire() {
		c.Wire.Frame = []int{2}
	}
	var sources []string
	for ch := range n {
		name := "C" + strconv.Itoa(ch+1)
		c.Channels = append(c.Channels, wire.CapsChannel{Ch: ch, Name: name, Color: "c" + strconv.Itoa(ch+1)})
		sources = append(sources, name)
	}
	f := p.Format
	st8 := "u8"
	if f.Storage == sample.U16 {
		st8 = "u16"
	}
	c.Codes = wire.CapsCodes{
		Bits: f.Bits, Storage: st8, Shift: f.Shift, Frac: f.Frac, Min: f.Min, Max: f.Max, Mid: f.Mid,
		CodesPerDiv: s.codesPerDiv(),
	}
	if s.v1Wire() {
		c.Codes.FracBitsPrecision = 8
	}
	c.Timebase = wire.CapsTimebase{
		Tdivs: p.Timebase.TdivS, FullRateTdivS: p.Timebase.FullRateTdivS, Positions: []float64{0, 1},
	}
	if r := fastestRate(p.Rates); r > 0 {
		c.Timebase.FullRateS = 1 / r
	}
	for _, step := range b.FrontEnd().Ladder(0) {
		c.Vertical.Vdivs = append(c.Vertical.Vdivs, step.VoltsDiv)
		if step.Zoom > 1 {
			c.Vertical.ZoomedVdivs = append(c.Vertical.ZoomedVdivs, step.VoltsDiv)
		}
	}
	c.Vertical.Probes = s.cfg.Probes
	for _, o := range b.FrontEnd().Couplings(0) {
		c.Vertical.Couplings = append(c.Vertical.Couplings, couplingName(int(o.Coupling)))
	}
	c.Vertical.BWLimits = s.cfg.BWLimits
	tc := b.Trigger().Caps()
	c.Trigger = wire.CapsTrigger{
		Sources: sources, Modes: []string{"auto", "normal", sweepSingle}, HoldoffS: [2]float64{0, 10},
	}
	trigSrc := hal.Source{}
	switch {
	case s.cfg.Instrument != nil:
		trigSrc = st.Trigger.Source()
		c.Trigger.Types, c.Trigger.Avail, c.Trigger.Params = trigTypeCaps(tc)
	case tc.Has(hal.NodeEdge) || len(tc.Nodes) == 0:
		c.Trigger.Types = []string{"edge"} // the trigger types the shared settings program
	}
	if len(st.Channels) > 0 {
		if g := b.Trigger().LevelGrid(trigSrc); g.Step > 0 && g.Max > g.Min {
			c.Trigger.LevelSteps = int((g.Max-g.Min)/g.Step + 0.5)
		}
	}
	c.Acquisition.Modes = []string{"normal", "average", "eres"}
	if p.Content.Has(sample.PeakPairs) {
		c.Acquisition.Modes = append(c.Acquisition.Modes, "peak")
	}
	for k := sample.RawFullRate; k <= sample.DisplayDomain; k++ {
		if p.Content.Has(k) {
			c.Acquisition.Content = append(c.Acquisition.Content, k.String())
		}
	}
	c.Features = s.features(p)
	c.Controls = sharedControls
	if s.cfg.Controls != nil {
		c.Controls = nil // the scope's own controls take its v1 names
	}
	c.Screen = wire.CapsScreen{W: p.Display.W, H: p.Display.H, Format: pixelFormat(p.Display.Format), Touch: p.Display.Touch}
	c.UI = wire.CapsUI{Keys: p.Inputs.Keys, Knobs: p.Inputs.Knobs}
	if ui, ok := s.UI(); ok && ui != nil {
		c.UI.Remote = true
	}
	c.Limits = wire.CapsLimits{FrameCols: [2]int{64, 4096}, LiveClients: p.Limits.WebClients, RecordMax: p.Limits.RecordMax}
	if c.Limits.LiveClients == 0 {
		c.Limits.LiveClients = 1
	}

	if s.cfg.CapsEdit != nil {
		s.cfg.CapsEdit(&c)
	}
	body, _ := json.Marshal(c)
	h := fnv.New64a()
	_, _ = h.Write(body)
	sum := h.Sum64()
	s.caps.mu.Lock()
	if sum != s.caps.hash || s.caps.gen == 0 {
		s.caps.hash = sum
		s.caps.gen++
	}
	c.Gen = s.caps.gen
	s.caps.mu.Unlock()
	return c
}

// trigTypeCaps are the trigger types the instrument state holds on a
// trigger with caps tc: the names of those that run, every type with why
// not, and the settings of each that runs.
// TRLC-LINKS: REQ-DSO-164, REQ-DSO-011
func trigTypeCaps(tc hal.TrigCaps) (names []string, avail []wire.CapsTrigType, params map[string][]wire.CapsTrigParam) {
	kinds := [...]string{"number", "int", "choice", "bool", "source", "states"}
	params = map[string][]wire.CapsTrigParam{}
	for t := range instrument.NumTrigTypes {
		ok, why := t.Avail(tc)
		id := t.String()
		avail = append(avail, wire.CapsTrigType{ID: id, OK: ok, Why: why})
		if !ok {
			continue
		}
		names = append(names, id)
		for _, p := range instrument.TrigParams(t) {
			params[id] = append(params[id], wire.CapsTrigParam{
				ID: p.ID, Kind: kinds[p.Kind], Unit: p.Unit, Choices: p.Choices, PerChannel: p.PerChannel,
			})
		}
	}
	return names, avail, params
}

// sharedControls are the settings v2 controls of the instrument model.
var sharedControls = []wire.CapsControl{
	{Name: "tdiv", Unit: "s", Ladder: "timebase.tdivs"},
	{Name: "vdiv", Unit: "V", PerChannel: true, Ladder: "vertical.vdivs"},
	{Name: "offset", Unit: "V", PerChannel: true},
	{Name: "probe", PerChannel: true, Ladder: "vertical.probes"},
	{Name: "coupling", PerChannel: true, Ladder: "vertical.couplings"},
	{Name: "bwlimit", PerChannel: true, Ladder: "vertical.bw_limits"},
	{Name: "invert", PerChannel: true},
	{Name: "on", PerChannel: true},
	{Name: "triglevel", Unit: "V"},
	{Name: "trigsource"},
	{Name: "trigslope"},
	{Name: "trigtype"},
	{Name: "trigpos"},
	{Name: "holdoff", Unit: "s"},
	{Name: "run"},
	{Name: "runtoggle"},
	{Name: "single"},
	{Name: "norm"},
	{Name: "memdepth"},
	{Name: "acqmode"},
}

// streamWhy is why a feature fed by a gap-free stream is absent: the
// board's own reason when it has no streamer, else why.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) streamWhy(p hal.Profile, why string) string {
	if b := s.cfg.Board; b != nil {
		if _, ok := b.Streamer(); !ok {
			return p.AbsentReason("streamer")
		}
	}
	return why
}

// zonesWhy is why the zone trigger is absent: on the HAL path, that the
// owner loop qualifies no trigger with zones (they are a board trigger's
// qualifier, hal.TrigStage.Zones), else why.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) zonesWhy(why string) string {
	if s.cfg.Board != nil && s.cfg.Instrument != nil {
		return "zones qualify a board's trigger (its stages' zones), which the core's owner loop does not program"
	}
	return why
}

// features is every optional feature, ok or why not.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) features(p hal.Profile) map[string]wire.Feature {
	parts := s.cfg.Parts
	why := func(ok bool, reason string) wire.Feature {
		if ok {
			return wire.Feature{OK: true}
		}
		return wire.Feature{Why: reason}
	}
	noEngine := "this scope's engine does not run it yet"
	raw := p.Content.Has(sample.RawFullRate)
	out := map[string]wire.Feature{
		"decode":        {OK: true},
		"measure":       {OK: true},
		"eye":           why(raw, "needs full-rate raw records"),
		"superres":      why(raw, "needs full-rate raw records"),
		"record":        why(true, ""),
		"client_decode": why(parts.ClientDecode != nil, s.streamWhy(p, noEngine)),
		"sound":         why(parts.Sound != nil, s.streamWhy(p, noEngine)),
		"bode":          why(parts.Bode != nil, noEngine),
		"mask":          why(parts.Masks != nil, noEngine),
		"zones":         why(parts.Zones != nil, s.zonesWhy(noEngine)),
		"serial":        why(parts.Serial != nil, noEngine),
		"ets":           {Why: "equivalent-time sampling is not available on this acquisition"},
	}
	if _, ok := s.Refs(); ok {
		out["refs"] = wire.Feature{OK: true, Slots: 2}
	} else {
		out["refs"] = wire.Feature{Why: "references are kept by the scope's UI model"}
	}
	if _, ok := s.Setups(); ok {
		out["setups"] = wire.Feature{OK: true, Slots: setupSlots}
	} else {
		out["setups"] = wire.Feature{Why: "this scope has no setup memory"}
	}
	if b := s.cfg.Board; b != nil {
		for _, part := range hal.OptionalParts {
			if !present(b, part) {
				out[part] = wire.Feature{Why: p.AbsentReason(part)}
			} else {
				out[part] = wire.Feature{OK: true}
			}
		}
		for _, x := range b.Extensions() {
			f := wire.Feature{OK: true}
			for _, r := range x.Routes {
				f.Routes = append(f.Routes, routePath(r.Pattern))
			}
			out[x.Feature] = f
		}
	}
	for _, r := range s.cfg.Routes {
		f := out[r.Feature]
		f.OK = true
		f.Routes = append(f.Routes, routePath(r.Pattern))
		out[r.Feature] = f
	}
	return out
}

// present reports a board's optional part by its profile name.
// TRLC-LINKS: REQ-DSO-164
func present(b hal.Board, part string) bool {
	var ok bool
	switch part {
	case "streamer":
		_, ok = b.Streamer()
	case "images":
		_, ok = b.Images()
	case "stacker":
		_, ok = b.Stacker()
	case "segments":
		_, ok = b.Segments()
	case "counter":
		_, ok = b.Counter()
	case "logic-probe":
		_, ok = b.LogicProbe()
	case "generator":
		_, ok = b.Generator()
	}
	return ok
}

// routePath is a ServeMux pattern's path ("POST /api/x" is /api/x).
// TRLC-LINKS: REQ-DSO-164
func routePath(pattern string) string {
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}

// codesPerDiv is the stored words a division: the newest frame's first
// trace's that states one, else the code range over the graticule's divisions down.
// TRLC-LINKS: REQ-DSO-164
func (s *Scope) codesPerDiv() float64 {
	var cpd float64
	s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		if f == nil {
			return
		}
		for i := range f.Traces { // a channel switched off states none
			if cpd = f.Traces[i].CodesPerDiv; cpd > 0 {
				return
			}
		}
	})
	if cpd > 0 {
		return cpd
	}
	p := s.profile()
	if p.Display.DivsV > 0 && p.Format.Max > p.Format.Min {
		return float64(p.Format.Max-p.Format.Min) / float64(p.Display.DivsV)
	}
	return 25
}

// fastestRate is the highest sample rate a profile lists.
// TRLC-LINKS: REQ-DSO-164
func fastestRate(rs []hal.Rate) float64 {
	var r float64
	for _, x := range rs {
		r = max(r, x.SamplesPerS)
	}
	return r
}

// pixelFormat is a framebuffer format as caps names it.
// TRLC-LINKS: REQ-DSO-164
func pixelFormat(f hal.PixelFormat) string {
	return [...]string{"", "RGB565", "XRGB8888", "ARGB8888"}[min(max(int(f), 0), 3)]
}
