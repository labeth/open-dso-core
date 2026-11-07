// Package meas is the SCPI measurement engine: the parameter table (name,
// unit, the measurement field, what a reading needs), the "no reading" rule
// and the edge-delay measurements between two channels. Dialects format the
// values their own way.
package meas

import (
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/measure"
)

// NoReading is the reading of a parameter the record cannot support.
const NoReading = "****"

// Param is one measurement parameter.
type Param struct {
	Name, Unit string
	// Timing parameters need a resolved period; Edge parameters need an
	// edge's shape, which a peak-detect envelope's bucket hides.
	Timing, Edge bool
	Get          func(r *measure.Result) float64
}

// Params are the parameters in their legacy order (PAVA? ALL).
var Params = []Param{
	{"PKPK", "V", false, false, func(r *measure.Result) float64 { return r.Vpp }},
	{"MAX", "V", false, false, func(r *measure.Result) float64 { return r.Vmax }},
	{"MIN", "V", false, false, func(r *measure.Result) float64 { return r.Vmin }},
	{"AMPL", "V", false, false, func(r *measure.Result) float64 { return r.Vampl }},
	{"TOP", "V", false, false, func(r *measure.Result) float64 { return r.Vtop }},
	{"BASE", "V", false, false, func(r *measure.Result) float64 { return r.Vbase }},
	{"MEAN", "V", false, false, func(r *measure.Result) float64 { return notAliased(r, r.Vmean) }},
	{"RMS", "V", false, false, func(r *measure.Result) float64 { return notAliased(r, r.Vrms) }},
	{"OVSP", "%", true, true, func(r *measure.Result) float64 { return r.Overshoot }},
	{"RPRE", "%", true, true, func(r *measure.Result) float64 { return r.Preshoot }},
	{"PER", "s", true, false, func(r *measure.Result) float64 { return r.Period }},
	{"FREQ", "Hz", true, false, func(r *measure.Result) float64 { return r.Freq }},
	{"PWID", "s", true, false, func(r *measure.Result) float64 { return r.PosWidthS }},
	{"NWID", "s", true, false, func(r *measure.Result) float64 { return r.NegWidthS }},
	{"RISE", "s", true, true, func(r *measure.Result) float64 { return r.RiseS }},
	{"FALL", "s", true, true, func(r *measure.Result) float64 { return r.FallS }},
	{"DUTY", "%", true, false, func(r *measure.Result) float64 { return r.Duty }},
	{"NDUTY", "%", true, false, func(r *measure.Result) float64 { return 100 - r.Duty }},
}

var byName = func() map[string]*Param {
	m := map[string]*Param{}
	for i := range Params {
		m[Params[i].Name] = &Params[i]
	}
	return m
}()

// Lookup is the parameter named name.
// TRLC-LINKS: REQ-DSO-024
func Lookup(name string) (*Param, bool) {
	p, ok := byName[name]
	return p, ok
}

// Reading is the parameter's value in r, false when the record cannot
// support it (no measurement, no resolved period, no edge, not finite).
// TRLC-LINKS: REQ-DSO-024
func (p *Param) Reading(r *measure.Result) (float64, bool) {
	show := r != nil
	switch {
	case !show:
	case p.Edge:
		// Edge shape needs an edge, not a period: one edge on screen (a fast
		// timebase) has a rise time.
		show = (!r.EnvTiming || r.EdgeShape) && (r.RiseS > 0 || r.FallS > 0)
		switch p.Name {
		case "RISE":
			show = show && r.RiseS > 0
		case "FALL":
			show = show && r.FallS > 0
		}
	case p.Timing:
		show = r.HasTiming
	}
	if !show {
		return 0, false
	}
	v := p.Get(r)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// Legacy is the reading in the legacy form "%.6E<unit>", or NoReading.
// TRLC-LINKS: REQ-DSO-024
func (p *Param) Legacy(r *measure.Result) string {
	if v, ok := p.Reading(r); ok {
		return fmt.Sprintf("%.6E%s", v, p.Unit)
	}
	return NoReading
}

// notAliased is v, or NaN when r is an envelope of a signal faster than its
// buckets, which holds no mean or RMS.
// TRLC-LINKS: REQ-DSO-024
func notAliased(r *measure.Result, v float64) float64 {
	if r.EnvAliased {
		return math.NaN()
	}
	return v
}
