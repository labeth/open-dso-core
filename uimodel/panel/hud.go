// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"math"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/render"
)

// FillHUD fills what the display shows of the UI model: the shown channels,
// the views, decode, the workspace UI and its notices, the references,
// the cursors, super-resolution and the mask's line.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-211
func (c *Controller) FillHUD(hud *render.HUD) {
	d, v := c.Snapshot() // the display state and the workspace UI, read together
	hud.Ch[0].Show, hud.Ch[1].Show, hud.ChanExplicit = d.ShowC1, d.ShowC2, true
	for ch, on := range d.ShowCh {
		if ch < len(hud.Ch) {
			hud.Ch[ch].Show = on
		}
	}
	hud.ShowMeas, hud.MeasItems = d.ShowMeas, d.MeasItems
	hud.ViewMode, hud.MathMode, hud.FFTSrc = d.ViewMode, d.MathMode, d.FFTSrc
	c.mu.Lock()
	hud.MathCh, hud.XYCh = c.mathCh, c.xyCh
	c.mu.Unlock()
	hud.AutosetBusy, hud.AutosetMsg = d.AutosetBusy, d.AutosetMsg
	hud.Notice, hud.Remote = v.Notice, d.Remote
	hud.UI = v
	hud.Match = render.MatchView(c.MatchEditor())
	hud.Zoom, hud.ZoomOff = d.Zoom, d.ZoomOff
	hud.Persist = d.Persist
	hud.DecProto, hud.DecBaud = d.DecProto, d.DecBaud
	hud.DecChA, hud.DecChB = d.DecChA, d.DecChB
	hud.DecCPOL, hud.DecCPHA, hud.DecLSB = d.DecCPOL, d.DecCPHA, d.DecLSB
	hud.DecParity, hud.DecInverted = d.DecParity, d.DecInverted
	hud.DecFormat, hud.DecDelayNs = d.DecFormat, d.DecDelayNs
	rv := c.RefView()
	for i := range 2 {
		hud.Ref[i], hud.RefShow[i] = rv[i].Ch, rv[i].Show
		hud.RefVdiv[i], hud.RefOff[i], hud.RefProbe[i] = rv[i].Vdiv, rv[i].Off, rv[i].Probe
	}
	hud.CurOn, hud.CurType, hud.CurSel = d.CurOn, d.CurType, d.CurSel
	hud.CurX, hud.CurY = d.CurX, d.CurY
	sv := c.SuperresView()
	hud.SRActive, hud.SRFocus, hud.SRStatus = sv.Active, sv.Focus, sv.Status
	hud.SRBits, hud.SRMean, hud.SRk = sv.Bits, sv.Mean, sv.K
	hud.SRMean2, hud.SRAlign, hud.SRSampleS = sv.Mean2, sv.Align, sv.SampleS
	hud.SRGateLo, hud.SRGateHi, hud.SRN = sv.GateLo, sv.GateHi, sv.N
	hud.SRWinLo, hud.SRWinHi, hud.SRPeriod = sv.WinLo, sv.WinHi, sv.Period
	hud.MaskMsg = c.MaskStatus()
}

// HUD is the display's whole input for a scope whose instrument state is
// the core's: the timebase, trigger and run state from the controls, each
// channel's scale, probe, coupling and offset from the front end and the
// levels, the trigger level's marker and readout in volts, and FillHUD's
// part. chans is the scope's channel count.
// TRLC-LINKS: REQ-DSO-168
func (c *Controller) HUD(chans int) render.HUD {
	st := c.eng.Snapshot()
	hud := render.HUD{
		TdivS: st.TdivS, TrigSrc: st.TrigSource, TrigRising: st.TrigRising,
		TrigType: st.TrigType, TrigQual: render.TrigQual(st.TrigQual), SerialArmed: st.SerialMode != 0, ZoneTrig: st.ZoneMode != 0,
		Running: st.Running, Norm: st.Norm, Single: st.Single, RecordS: st.RecordS,
		TrigPosFrac: st.TrigPosFrac, NumChans: min(max(chans, 1), render.MaxChans),
		ViewPending: st.ViewPending, Roll: st.Roll,
	}
	for ch := range hud.NumChans {
		hud.Ch[ch] = render.Channel{VdivV: 1, Show: true}
	}
	if c.fe != nil {
		for ch := range min(hud.NumChans, c.nch) {
			k := &hud.Ch[ch]
			k.VdivV, k.Probe, k.Cpl = c.fe.VdivV(ch), c.fe.ProbeFactor(ch), c.fe.Coupling(ch)
			if v := k.VdivV; v > 0 {
				k.Zoom = max(1, int(math.Round(c.fe.RangeV(ch)/v))) // a step drawn zoomed from its analog range
			}
			if k.Cpl != dsp.CoupleAC { // AC removes the DC in software: the ground marker sits at centre
				k.OffV = c.lv.OffsetIn(ch, st)
			}
		}
	}
	if src := st.TrigSource; src >= 0 && src < hud.NumChans && hud.Ch[src].VdivV > 0 {
		v := c.lv.TrigLevelOrIn(st, 0, src)
		probe := max(hud.Ch[src].Probe, 1)
		hud.TrigLvlV = v
		hud.TrigLvlDiv = (v/probe + hud.Ch[src].OffV) / hud.Ch[src].VdivV
	}
	if st.Busy != "" && st.BusyMs >= 150 {
		hud.Busy, hud.BusyPct, hud.BusyPhase = st.Busy, st.BusyPct, int(st.BusyMs/250)
	}
	hud.MaskMode, hud.MaskPass, hud.MaskFail, hud.MaskSkip = st.MaskMode, st.MaskPass, st.MaskFail, st.MaskSkip
	hud.MaskStopped = st.MaskStopped
	c.FillHUD(&hud)
	return hud
}
