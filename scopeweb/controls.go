// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/webserver"
)

// controls is /api/set over the instrument model: each settings v2 control
// edits the wanted state, which the owner loop commits at its next commit
// point. What the board can tell without hardware (a ladder, a grid) is
// checked here; what only the hardware can refuse comes back through the
// instrument's refusal listeners.
type controls struct{ s *Scope }

// errUnknown is a control this scope does not have.
var errUnknown = errors.New("unknown control")

// Set applies one control.
// TRLC-LINKS: REQ-DSO-162
func (c controls) Set(_ context.Context, req webserver.Control) webserver.Applied {
	s := c.s
	m := s.cfg.Instrument
	if m == nil || s.cfg.Board == nil {
		return webserver.Applied{Err: "this scope takes no settings from the page"}
	}
	nch := len(m.State().Channels)
	if webserver.PerChannel[req.Name] && (req.Ch < 0 || req.Ch >= nch) {
		return webserver.Applied{Err: fmt.Sprintf("%s: no channel %d on this scope (it has %d)", req.Name, req.Ch, nch)}
	}
	applied, brief, err := c.apply(m, req)
	if err != nil {
		return webserver.Applied{Err: refusalText(err)}
	}
	return webserver.Applied{OK: true, Value: applied, Brief: brief}
}

// refusalText is a refusal as the page shows it.
// TRLC-LINKS: REQ-DSO-162
func refusalText(err error) string {
	if r, ok := errors.AsType[*hal.Refusal](err); ok {
		return r.Error()
	}
	return err.Error()
}

// apply edits the wanted state for one control and returns the value it
// took (snapped to the board's ladders and grids) and whether the reply
// is brief (a control that cannot be refused).
// TRLC-LINKS: REQ-DSO-162
func (c controls) apply(m *instrument.Model, req webserver.Control) (any, bool, error) { //nolint:gocyclo // one case per control
	b := c.s.cfg.Board
	v := req.Value
	var applied any = v
	edit := func(fn func(st *instrument.State) error) error { return m.Change(fn) }
	switch req.Name {
	case "run":
		return v, false, edit(func(st *instrument.State) error {
			st.Running = v != 0
			if st.Running && st.Sweep == hal.Single {
				st.Sweep = hal.Auto
			}
			return nil
		})
	case "runtoggle":
		on := false
		err := edit(func(st *instrument.State) error {
			st.Running = !st.Running
			on = st.Running
			return nil
		})
		if on {
			return 1.0, false, err
		}
		return 0.0, false, err
	case sweepSingle:
		return v, false, edit(func(st *instrument.State) error { st.Sweep, st.Running = hal.Single, true; return nil })
	case "norm":
		return v, false, edit(func(st *instrument.State) error {
			st.Sweep = hal.Auto
			if v != 0 {
				st.Sweep = hal.NormalSweep
			}
			return nil
		})
	case "tdiv":
		t, ok := onLadder(c.s.profile().Timebase.TdivS, v)
		if !ok {
			return nil, false, errors.New("tdiv not in ladder")
		}
		return t, false, edit(func(st *instrument.State) error { st.TimeDivS = t; return nil })
	case "trigpos":
		if !(v >= 0 && v <= 1) {
			return nil, false, errors.New("trigpos is a screen fraction, 0 to 1")
		}
		divs := c.s.divsH()
		return v, false, edit(func(st *instrument.State) error { st.DelayS = (0.5 - v) * divs * st.TimeDivS; return nil })
	case "holdoff":
		h := max(v, 0)
		return h, true, edit(func(st *instrument.State) error { st.Trigger.HoldoffS = h; return nil })
	case "memdepth":
		d := max(int(v), 0)
		if lim := c.s.profile().MaxDepth; lim > 0 {
			d = min(d, lim)
		}
		return d, true, edit(func(st *instrument.State) error { st.Depth = d; return nil })
	case "acqmode":
		mode, ok := acqModes[int(v)]
		if !ok || float64(int(v)) != v {
			return nil, false, fmt.Errorf("acquisition mode %g: this scope takes 0 normal, 1 average, 2 high resolution, 3 peak detect", v)
		}
		return v, false, edit(func(st *instrument.State) error { st.Mode = mode; return nil })
	case "trigsource":
		ch := int(v)
		if req.Text != "" {
			ch = channelOf(req.Text)
		}
		if ch < 0 || ch >= len(m.State().Channels) || float64(int(v)) != v && req.Text == "" {
			return nil, false, fmt.Errorf("trigger source %g: not one of this scope's channels", v)
		}
		return ch, false, edit(func(st *instrument.State) error {
			st.Trigger.Source = hal.Source{Kind: hal.Analog, Index: ch}
			return nil
		})
	case "trigslope":
		return v, false, edit(func(st *instrument.State) error {
			st.Trigger.Slope = hal.Falling
			if v != 0 {
				st.Trigger.Slope = hal.Rising
			}
			return nil
		})
	case "triglevel":
		var lv float64
		err := edit(func(st *instrument.State) error {
			src := st.Trigger.Source
			if req.Ch >= 0 && (src.Kind != hal.Analog || src.Index != req.Ch) {
				return fmt.Errorf("triglevel: the trigger's source is not channel %d", req.Ch)
			}
			lv = b.Trigger().LevelGrid(src).Snap(v)
			st.Trigger.LevelV = lv
			return nil
		})
		return lv, false, err
	case "trigtype":
		if v != 0 && !strings.EqualFold(req.Text, "edge") {
			return nil, false, errors.New("this scope's shared trigger settings take the edge trigger only")
		}
		return 0.0, false, nil
	case "triglevelcode":
		return nil, false, errors.New("the trigger level is set in volts on this scope (triglevel)")
	case "vdiv":
		var vd float64
		err := edit(func(st *instrument.State) error {
			ch := &st.Channels[req.Ch]
			steps := b.FrontEnd().Ladder(req.Ch)
			var ladder []float64
			for _, s := range steps {
				ladder = append(ladder, s.VoltsDiv)
			}
			got, ok := onLadder(ladder, v)
			if !ok {
				return errors.New("vdiv not in ladder")
			}
			ch.VoltsDiv, vd = got, got
			return nil
		})
		return vd, false, err
	case "offset":
		var off float64
		err := edit(func(st *instrument.State) error {
			off = b.FrontEnd().OffsetGrid(req.Ch).Snap(v)
			st.Channels[req.Ch].OffsetV = off
			return nil
		})
		return off, false, err
	case "probe":
		if !(v > 0) || !containsF(c.s.cfg.Probes, v) {
			return nil, false, fmt.Errorf("probe must be one of %s", listF(c.s.cfg.Probes))
		}
		return v, false, edit(func(st *instrument.State) error {
			ch := &st.Channels[req.Ch]
			old := ch.Probe
			if !(old > 0) {
				old = 1
			}
			ch.VoltsDiv *= v / old // the same analog range: V/div at the tip follows the probe
			ch.OffsetV *= v / old
			ch.Probe = v
			return nil
		})
	case "coupling":
		cp, ok := couplingOf(req)
		if !ok {
			return nil, false, errors.New("coupling must be 0 (DC), 1 (AC) or 2 (GND)")
		}
		if !hasCoupling(b.FrontEnd().Couplings(req.Ch), cp) {
			return nil, false, fmt.Errorf("channel C%d has no %s coupling", req.Ch+1, couplingName(int(cp)))
		}
		return float64(cp), false, edit(func(st *instrument.State) error { st.Channels[req.Ch].Coupling = cp; return nil })
	case "bwlimit":
		hz, ok := bwLimitOf(c.s.cfg.BWLimits, v)
		if !ok {
			return nil, false, fmt.Errorf("bandwidth limit %g: this scope offers %s", v, strings.Join(c.s.cfg.BWLimits, ", "))
		}
		return v, false, edit(func(st *instrument.State) error { st.Channels[req.Ch].BWLimit = hz; return nil })
	case "invert":
		return v, false, edit(func(st *instrument.State) error { st.Channels[req.Ch].Invert = v != 0; return nil })
	case "on":
		return v, false, edit(func(st *instrument.State) error { st.Channels[req.Ch].On = v != 0; return nil })
	}
	if _, ok := otherControls[req.Name]; ok {
		return nil, false, fmt.Errorf("%s: not available on this scope", req.Name)
	}
	return applied, false, errUnknown
}

// otherControls are the v1 controls of features the shared settings do not
// carry: refused with the reason, not as unknown.
var otherControls = map[string]struct{}{
	"ets": {}, "rollstream": {}, "precisionrate": {}, "frameperiod": {}, "decodeview": {}, "stream": {},
	"serialmode": {}, "zonemode": {}, "maskmode": {}, "maskclear": {}, "bodemode": {}, "bodeclear": {},
	"pulseparams": {}, "slopeparams": {}, "videoparams": {}, "avgcount": {}, "eres": {}, "eresbits": {},
}

// acqModes are the v1 acquisition mode numbers.
var acqModes = map[int]hal.AcqMode{0: hal.Normal, 1: hal.Average, 2: hal.HighRes, 3: hal.PeakDetect}

// onLadder is the ladder's entry v names (within a part in a million).
// TRLC-LINKS: REQ-DSO-162
func onLadder(ladder []float64, v float64) (float64, bool) {
	for _, l := range ladder {
		if math.Abs(l-v) <= 1e-6*math.Abs(l) {
			return l, true
		}
	}
	return 0, false
}

// TRLC-LINKS: REQ-DSO-162
func containsF(xs []float64, v float64) bool {
	_, ok := onLadder(xs, v)
	return ok
}

// TRLC-LINKS: REQ-DSO-162
func listF(xs []float64) string {
	var parts []string
	for _, x := range xs {
		parts = append(parts, fmt.Sprintf("%g", x))
	}
	return strings.Join(parts, ", ")
}

// channelOf is a channel's index by name ("C3" is 2); -1 for another name.
// TRLC-LINKS: REQ-DSO-162
func channelOf(name string) int {
	var n int
	if _, err := fmt.Sscanf(strings.ToUpper(name), "C%d", &n); err != nil || n < 1 {
		return -1
	}
	return n - 1
}

// couplingOf is a coupling by number (0 DC, 1 AC, 2 GND) or name.
// TRLC-LINKS: REQ-DSO-162
func couplingOf(req webserver.Control) (hal.Coupling, bool) {
	switch strings.ToUpper(req.Text) {
	case "DC":
		return hal.DC, true
	case "AC":
		return hal.AC, true
	case "GND":
		return hal.GND, true
	case "":
	default:
		return 0, false
	}
	switch req.Value {
	case 0:
		return hal.DC, true
	case 1:
		return hal.AC, true
	case 2:
		return hal.GND, true
	}
	return 0, false
}

// TRLC-LINKS: REQ-DSO-162
func hasCoupling(opts []hal.CouplingOption, c hal.Coupling) bool {
	for _, o := range opts {
		if o.Coupling == c {
			return true
		}
	}
	return false
}

// bwLimitOf is the bandwidth limit the v1 value names: 0 full, k the k-th
// limit the scope lists after "full" ("20M" is 20 MHz).
// TRLC-LINKS: REQ-DSO-162
func bwLimitOf(names []string, v float64) (float64, bool) {
	k := int(v)
	if float64(k) != v || k < 0 || k >= len(names) {
		return 0, false
	}
	return hzOf(names[k]), true
}

// hzOf is a bandwidth's name in hertz ("20M", "200k"); 0 for "full".
// TRLC-LINKS: REQ-DSO-162
func hzOf(name string) float64 {
	var x float64
	var unit string
	if n, _ := fmt.Sscanf(name, "%g%s", &x, &unit); n < 1 {
		return 0
	}
	switch strings.ToUpper(unit) {
	case "K":
		x *= 1e3
	case "M":
		x *= 1e6
	case "G":
		x *= 1e9
	}
	return x
}
