// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"time"

	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/settings"
)

// RefsState is both reference slots as the references' store keeps them
// (settings.RefsKey), every channel the scope has.
// TRLC-LINKS: REQ-DSO-138
func (c *Controller) RefsState() settings.Refs {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := settings.Refs{Version: settings.RefsVersion}
	for i, r := range c.refs {
		if !r.has {
			continue
		}
		n := max(c.nch, 2)
		o := settings.RefSlot{
			Has: true, Show: r.show, TdivS: r.tdiv, PosFrac: r.posFrac, At: r.at.UnixMilli(),
			Cols: make([][]byte, n), VdivV: make([]float64, n), OffV: make([]float64, n),
			Probe: make([]float64, n), CPD: make([]float64, n),
		}
		for ch := range n {
			if r.cs[ch] != nil {
				o.Cols[ch] = append([]byte(nil), r.cs[ch]...)
			}
			o.VdivV[ch], o.OffV[ch], o.Probe[ch], o.CPD[ch] = r.vdiv[ch], r.off[ch], r.probe[ch], r.cpd[ch]
		}
		if s := r.setup; s.has {
			o.Setup = &settings.RefSetupSaved{
				TdivS: s.tdiv, PosFrac: s.posFrac, TrigLevel: s.trigLevel.Word, TrigSrc: s.trigSrc, Rising: s.rising,
				TrigType: s.trigType, VdivV: make([]float64, n), RangeV: make([]float64, n), OffReqV: make([]float64, n),
			}
			for ch := range n {
				o.Setup.VdivV[ch], o.Setup.RangeV[ch], o.Setup.OffReqV[ch] = s.vert[ch].vdiv, s.vert[ch].rng, s.offReq[ch]
			}
		}
		out.Slots[i] = o
	}
	return out
}

// RefsGen moves whenever a reference is stored, shown, hidden or cleared.
// TRLC-LINKS: REQ-DSO-138
func (c *Controller) RefsGen() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	h := newSig()
	for _, r := range c.refs {
		h.u64(uint64(b2ic(r.has)) | uint64(b2ic(r.show))<<1) //nolint:gosec // G115: 0 to 3
		h.u64(r.gen)
	}
	return uint64(h)
}

// ApplyRefs puts stored references back into the slots: each channel the
// scope has, a column count other than RefCols dropped.
// TRLC-LINKS: REQ-DSO-138
func (c *Controller) ApplyRefs(in settings.Refs) {
	c.mu.Lock()
	for i, o := range in.Slots {
		if !o.Has {
			c.refs[i] = refWave{}
			continue
		}
		r := refWave{has: true, show: o.Show, tdiv: o.TdivS, posFrac: o.PosFrac}
		if o.At > 0 {
			r.at = time.UnixMilli(o.At)
		}
		at := func(xs []float64, ch int) float64 {
			if ch < len(xs) {
				return xs[ch]
			}
			return 0
		}
		for ch := range c.nch {
			if ch < len(o.Cols) && len(o.Cols[ch]) == RefCols {
				r.cs[ch] = append([]uint8(nil), o.Cols[ch]...)
			}
			r.vdiv[ch], r.off[ch], r.probe[ch], r.cpd[ch] = at(o.VdivV, ch), at(o.OffV, ch), max(at(o.Probe, ch), 1), at(o.CPD, ch)
		}
		if s := o.Setup; s != nil {
			r.setup = cmpSetup{
				has: true, tdiv: s.TdivS, posFrac: s.PosFrac, trigLevel: instrument.TrigLevelSaved{Word: s.TrigLevel},
				trigSrc: c.chOf(s.TrigSrc), rising: s.Rising, trigType: s.TrigType & 3,
			}
			for ch := range c.nch {
				r.setup.vert[ch] = vstep{vdiv: at(s.VdivV, ch), rng: at(s.RangeV, ch)}
				r.setup.offReq[ch] = at(s.OffReqV, ch)
			}
		}
		c.refGen++
		r.gen = c.refGen
		c.refs[i] = r
	}
	c.mu.Unlock()
}
