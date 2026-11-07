// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/labeth/open-dso-core/dsp"
	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/measure"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/shape"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// Frames is the scope's frame source for /api/frame.bin.
// TRLC-LINKS: REQ-DSO-163
func (s *Scope) Frames() webserver.Frames {
	fr := frames{s}
	if !s.v1Wire() {
		return framesV2Only{fr}
	}
	if s.cfg.Quiet != nil {
		return quietFrames{fr}
	}
	return fr
}

// frames serves the fan-out's frames.
type frames struct{ s *Scope }

// quietFrames are frames whose serialisation waits out the acquisition's
// load-sensitive windows.
type quietFrames struct{ frames }

// TRLC-LINKS: REQ-DSO-163
func (q quietFrames) QuietRLock() { q.s.cfg.Quiet.QuietRLock() }

// TRLC-LINKS: REQ-DSO-163
func (q quietFrames) QuietRUnlock() { q.s.cfg.Quiet.QuietRUnlock() }

// framesV2Only are the frames of a scope v1 cannot carry.
type framesV2Only struct{ frames }

// V2Only marks frames v1 cannot carry.
// TRLC-LINKS: REQ-DSO-163
func (framesV2Only) V2Only() {}

// Wait parks until the fan-out snapshots a frame after since, or timeout.
// TRLC-LINKS: REQ-DSO-163
func (fr frames) Wait(since uint64, timeout time.Duration) {
	fr.s.cfg.Frames.WaitNext(since, timeout)
}

// Frame builds the v1 reply for q from the newest frame, with the current
// scales and trigger position; measurements reuse the previous frame's for
// 100 ms while running free (not on a single capture or stopped).
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func (fr frames) Frame(q webserver.FrameQuery) wire.FrameReply {
	s := fr.s
	sc := s.scales()
	off, vpc := sc.Scales()
	posFrac, running, single := sc.Position()
	if !(posFrac >= 0 && posFrac <= 1) {
		posFrac = 0.5
	}
	var rep wire.FrameReply
	s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		rep = s.BuildReply(f, ReplyQuery{Cols: q.Cols, Full: q.Full, Since: q.Since, Off: off, Vpc: vpc, PosFrac: posFrac, MeasThrottle: running && !single})
	})
	return rep
}

// RawFrame builds the raw shape (?raw=1).
// TRLC-LINKS: REQ-DSO-163
func (fr frames) RawFrame(since uint64) wire.RawFrame { return fr.s.rawFrame(since) }

// Lease renews or ends one of the engine's consumer leases.
// TRLC-LINKS: REQ-DSO-163
func (fr frames) Lease(n webserver.Need) bool {
	if l := fr.s.cfg.Parts.Leases; l != nil {
		return l.Lease(n)
	}
	return false
}

// FrameV2 fills dst for q: a v1 scope's reply in wire v2, or the newest
// frame's whole record with its traces as they were read.
// TRLC-LINKS: REQ-DSO-163
func (fr frames) FrameV2(q webserver.FrameQueryV2, dst *wire.Frame) {
	s := fr.s
	if s.v1Wire() {
		if !q.Raw {
			dst.FromReply(fr.Frame(q.FrameQuery))
			return
		}
		dst.FromReply(rawReply(fr.RawFrame(q.Since)))
		dst.Header.Raw = true
		return
	}
	st := s.state()
	s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		if f == nil || f.Seq == 0 || f.Seq == q.Since || f.Len() == 0 {
			var seq uint64
			if f != nil {
				seq = f.Seq
			}
			dst.Header = wire.FrameHeader{Seq: seq, Unchanged: true, EdgeX: -1, TriggerAt: -1}
			return
		}
		dst.FromSample(f)
		n := f.Len()
		h := &dst.Header
		h.Raw = q.Raw
		h.Cols, h.Depth, h.EdgeX = n, n, f.TriggerAt
		h.ColSpanS, h.DtS = float64(n)*f.SampleS, f.SampleS
		h.EdgeFrac, h.WinFrac = -1, 1
		if f.TriggerAt >= 0 {
			h.EdgeFrac = f.TriggerAt / float64(n)
		}
		if f.WinPoints > 0 && f.WinPoints < n {
			h.WinFrac = float64(f.WinPoints) / float64(n)
		}
		h.Held = shape.HeldRanges(f.Held(0), n)
		h.Ptp = f.SourcePtp
		for i := range dst.Traces {
			tr := &dst.Traces[i]
			t := &f.Traces[i]
			cpl := 0
			if t.Ch >= 0 && t.Ch < len(st.Channels) {
				cpl = int(st.Channels[t.Ch].Coupling)
			}
			tr.Coupling = couplingName(cpl)
			if q.Raw || f.Content == sample.PeakPairs {
				continue
			}
			tr.Clip = measure.Clipped(t.View8())
			tr.Meas = traceMeas(f, t, cpl)
		}
	})
}

// traceMeas measures one trace on its 8-bit view at its own codes a
// division, in volts at the probe tip.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func traceMeas(f *sample.Frame, t *sample.Trace, cpl int) *measure.Result {
	cpd := t.CodesPerDiv8()
	if !(cpd > 0) || !(t.Scale.PerCode != 0) {
		return nil
	}
	unit := t.Format.Unit8()
	vpc := t.Scale.PerCode * unit
	off := -(float64(t.Format.Mid) - t.Scale.Zero) * t.Scale.PerCode
	lo, hi := measure.Range(f)
	return measure.ComputeTrace(t, lo, hi, vpc, off+t.Scale.OffsetV, f.SampleS, cpl, f.Cond.Guard)
}

// couplingName is a coupling as the wire names it.
// TRLC-LINKS: REQ-DSO-163
func couplingName(c int) string {
	switch c {
	case 1:
		return "AC"
	case 2:
		return "GND"
	}
	return "DC"
}

// rawReply is a raw frame as the v1 reply it carries, its codes in C1/C2.
// TRLC-LINKS: REQ-DSO-163
func rawReply(raw wire.RawFrame) wire.FrameReply {
	rep := raw.Header
	if raw.Unchanged || raw.Precision {
		return rep
	}
	n := len(raw.Payload) / 2
	rep.C1, rep.C2 = make([]int16, n), make([]int16, n)
	for i := range n {
		rep.C1[i], rep.C2[i] = int16(raw.Payload[i]), int16(raw.Payload[n+i])
	}
	return rep
}

// ReplyQuery is what a v1 reply is built for: the page's columns, whether
// it wants the whole record, the frame it holds, the channels' scales,
// where the trigger sits and whether measurements may lag 100 ms.
type ReplyQuery struct {
	Cols         int
	Full         bool
	Since        uint64
	Off, Vpc     [2]float64
	PosFrac      float64
	MeasThrottle bool
}

// BuildReply assembles the v1 frame reply (binary header + raw payload)
// of a two-channel 8-bit frame. It must run inside the fan-out's
// WithFrame (f is valid only under its read lock); the reply owns all its
// data. MeasThrottle permits reusing measurements up to 100 ms old on a
// seq advance (running free only: a single capture or a stopped record is
// measured exactly).
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-164
func (s *Scope) BuildReply(f *sample.Frame, q ReplyQuery) wire.FrameReply {
	cols, since, off, vpc, posFrac := q.Cols, q.Since, q.Off, q.Vpc, q.PosFrac
	if f == nil || f.Seq == 0 || f.Seq == since {
		seq := uint64(0)
		if f != nil {
			seq = f.Seq
		}
		return wire.FrameReply{Seq: seq, Unchanged: true, EdgeX: -1}
	}
	// Clamp Valid to what the sample slices actually hold. Valid > len is an
	// engine-invariant violation, but every downstream f.C1[:f.Valid] would
	// panic the serve goroutine and crash the UI on one bad frame (frame-serve
	// fuzz). Clamp ONCE here so every path below is safe. Work on a shallow
	// copy with traces of its own: the fan-out snapshot is shared read-only
	// across clients.
	if f.Len() > len(f.Codes8(0)) || f.Len() > len(f.Codes8(1)) {
		fc := *f
		fc.Traces = append([]sample.Trace(nil), f.Traces...)
		if fc.Len() > len(fc.Codes8(0)) {
			fc.SetLen(len(fc.Codes8(0)))
		}
		if fc.Len() > len(fc.Codes8(1)) {
			fc.SetLen(len(fc.Codes8(1)))
		}
		f = &fc
	}
	// Coupling display transform (software, on this copy): AC removes the
	// DC (mean → mid-scale), GND shows a flat ground trace; both drop that
	// channel's offset so the baseline sits at centre. DC passes through.
	// Envelope frames are left untouched.
	sc := s.scales()
	c1, c2 := f.Codes8(0), f.Codes8(1)
	moff := off
	var cpl [2]int
	if f.Len() > 0 && !f.Flags.Has(sample.Envelope) {
		cpl[0], cpl[1] = sc.Coupling(0), sc.Coupling(1)
		if cpl[0] != 0 {
			c1, moff[0] = dsp.CoupleDisplay(f.Codes8(0)[:f.Len()], cpl[0]), 0
		}
		if cpl[1] != 0 {
			c2, moff[1] = dsp.CoupleDisplay(f.Codes8(1)[:f.Len()], cpl[1]), 0
		}
	}
	rep := wire.FrameReply{
		Seq:            f.Seq,
		WinCols:        min(f.WinPoints, f.Len()),
		SampleS:        f.SampleS,
		NoiseGainIdeal: f.Cond.NoiseGain, PassbandHz: f.Cond.PassbandHz, BandwidthHz: f.Cond.BandwidthHz, FilterGuard: f.Cond.Guard, Filter: f.Cond.Filter, Decimation: uint32(f.AcqDecim), FullRate: f.Content == sample.RawFullRate, CaptureDepth: f.Depth, TriggerKind: f.TriggerName(), //nolint:gosec // G115: AcqDecim holds a fabric's 32-bit decimation word
		EdgeX:          f.TriggerAt,
		Ptp:            f.SourcePtp,
		TdivS:          f.Snapshot.TdivS,
		DisplayedS:     f.Snapshot.DisplayedS,
		Interp:         f.Flags.Has(sample.Interp),
		Norm:           f.Snapshot.Norm,
		Trigd:          f.Status.Triggered,
		Coherent:       f.Status.Coherent,
		Degraded:       f.Status.Degraded,
		PeakDetect:     f.Content == sample.PeakPairs,
		Ordered:        f.Flags.Has(sample.Ordered),
		CaptureSampleS: captureSampleS(f),
		Cols:           cols,
		ColSpanS:       f.Snapshot.DisplayedS * 10, // the window spans 10 divisions
		Vpc1:           vpc[0], Vpc2: vpc[1],
		Off1V: moff[0], Off2V: moff[1],
	}
	// ColSpanS stays the 10-div screen time except on the deep path below,
	// which overrides it to the full-record time.
	switch {
	case f.Flags.Has(sample.Envelope):
		rep.IsEnv = true
		rep.E1Min = shape.ResampleEnv(f.Min8(0), len(f.Min8(0)), cols)
		rep.E1Max = shape.ResampleEnv(f.Max8(0), len(f.Min8(0)), cols)
		rep.E2Min = shape.ResampleEnv(f.Min8(1), len(f.Min8(0)), cols)
		rep.E2Max = shape.ResampleEnv(f.Max8(1), len(f.Min8(0)), cols)
		rep.EdgeFrac, rep.WinFrac = -1, 1
	case q.Full && (f.Depth > 0 || (!f.Flags.Has(sample.Interp) && f.Len() > f.WinPoints)):
		// A deep record: serve the whole record verbatim (not re-centred) and
		// report the trigger's real position (edge_frac = EdgeX/n). The page
		// centres and windows it, so the display, the super-res gate and the
		// raw-fed stacker share one coordinate system, the record's. The
		// record is anchored on the hardware trigger, so the page homes it
		// once per acquisition instead of chasing the edge; col_span_s is the
		// whole record's time so the page's Nyquist, Δt, decode and CSV agree.
		n := f.Len()
		rep.C1, rep.C2 = shape.RawInt16(c1[:n]), shape.RawInt16(c2[:n])
		if len(f.Precision(0)) == n && len(f.Precision(1)) == n {
			rep.Q1 = shape.CoupleQ8(f.Precision(0), cpl[0])
			rep.Q2 = shape.CoupleQ8(f.Precision(1), cpl[1])
			rep.FractionBits = 8
		}
		if f.TriggerAt >= 0 {
			rep.EdgeFrac = f.TriggerAt / float64(n)
		} else {
			rep.EdgeFrac = -1
		}
		rep.Cols, rep.ColSpanS, rep.Depth = n, float64(n)*f.SampleS, n
		rep.DtS = f.SampleS
		rep.WinFrac = float64(f.WinPoints) / float64(n)
		rep.Held = shape.HeldRanges(f.Held(0), n)
		if d := engine.ViewDetail(f, engine.ViewWeb); d != nil && f.Content == sample.PeakPairs && n > 1 {
			v, vn := d.Frame, d.Frame.Len()
			if vn > 1 && vn <= len(v.Codes8(0)) && vn <= len(v.Codes8(1)) && d.To > d.From {
				last := d.To - (d.To-d.From)/float64(vn) // where its last value lies
				rep.ViewA, rep.ViewB, rep.ViewN = d.From/float64(n-1), last/float64(n-1), vn
				rep.VC1 = shape.RawInt16(dsp.CoupleDisplayBy(v.Codes8(0)[:vn], f.Codes8(0)[:n], cpl[0]))
				rep.VC2 = shape.RawInt16(dsp.CoupleDisplayBy(v.Codes8(1)[:vn], f.Codes8(1)[:n], cpl[1]))
			}
		}
	default:
		// A record no deeper than the screen: the windowed screen slice.
		rep.C1 = shape.Window(c1, f.Len(), f.WinPoints, f.TriggerAt, f.Flags.Has(sample.Interp), cols, posFrac)
		rep.C2 = shape.Window(c2, f.Len(), f.WinPoints, f.TriggerAt, f.Flags.Has(sample.Interp), cols, posFrac)
		// True per-column time: the window really spans min(WinCols, Valid)
		// samples (mirrors Window's clamp) at SampleS each — not
		// col_span_s/cols, which is nominal on interpolated timebases.
		if win := f.WinPoints; f.Len() >= 1 {
			if win > f.Len() {
				win = f.Len()
			}
			rep.DtS = float64(win) * f.SampleS / float64(cols)
		}
		rep.WinFrac = 1
		if f.TriggerAt >= 0 {
			rep.EdgeFrac = posFrac
		} else {
			rep.EdgeFrac = -1
		}
	}
	rep.StreamSeq, rep.WindowNs, rep.GapNs = f.Stream.Seq, int64(math.Round(f.Stream.WindowS*1e9)), int64(math.Round(f.Stream.GapS*1e9))
	if f.Match != nil && rep.EdgeFrac >= 0 {
		end := f.Match.EndS
		rep.MatchEndS = &end
	}
	// Measurements over the raw record (accurate, band-independent),
	// computed once per published frame via the value-keyed cache: repeat
	// requests for the same seq (several clients, or a v2 and a v1 read)
	// reuse it.
	key := shape.MeasKey{Seq: f.Seq, Cpl1: cpl[0], Cpl2: cpl[1], Vpc: vpc, Off: moff, SampleS: f.SampleS}
	mv := s.meas.Get(key, q.MeasThrottle, func() shape.MeasVal {
		a, b := measure.Range(f) // a roll view's captured stretch
		qa := func(q []uint16) []uint16 {
			if len(q) < b {
				return nil
			}
			return q[a:b]
		}
		v := shape.MeasVal{
			M1:    measure.ComputeAcquisition(c1[a:b], qa(f.Precision(0)), vpc[0], moff[0], f.SampleS, cpl[0], f.Cond.Guard),
			M2:    measure.ComputeAcquisition(c2[a:b], qa(f.Precision(1)), vpc[1], moff[1], f.SampleS, cpl[1], f.Cond.Guard),
			Clip1: measure.Clipped(f.Codes8(0)[:f.Len()]), // raw rail state (pre-coupling)
			Clip2: measure.Clipped(f.Codes8(1)[:f.Len()]),
			Seq:   f.Seq, EdgeRead: engine.EdgeRead(f),
		}
		if f.Content == sample.PeakPairs {
			v.M1 = v.M1.WithEnvelopeTiming(c1[:f.Len()], vpc[0], moff[0], f.SampleS).WithEdgeDetail(vpc[0], moff[0], measure.EdgeWindows(f, 0)...)
			v.M2 = v.M2.WithEnvelopeTiming(c2[:f.Len()], vpc[1], moff[1], f.SampleS).WithEdgeDetail(vpc[1], moff[1], measure.EdgeWindows(f, 1)...)
		}
		return v
	})
	rep.M1, rep.M2, rep.Clip1, rep.Clip2 = mv.M1, mv.M2, mv.Clip1, mv.Clip2
	rep.MeasSeq, rep.EdgeRead = mv.Seq, mv.EdgeRead
	inv := sc.Inverted() // measure what the inverted trace shows
	if inv[0] {
		rep.M1 = rep.M1.Inverted()
	}
	if inv[1] {
		rep.M2 = rep.M2.Inverted()
	}
	return rep
}

// captureSampleS reports a frame's converter interval when it is not sample_s.
// TRLC-LINKS: REQ-DSO-163
func captureSampleS(f *sample.Frame) float64 {
	if f.Content == sample.PeakPairs && f.CaptureS > 0 && f.CaptureS != f.SampleS {
		return f.CaptureS
	}
	return 0
}

// rawFrame assembles the raw-shape frame (?raw=1): the un-windowed,
// un-interpolated, pre-coupling record — what the page's super-resolution
// stacker aligns and drizzles. Payload = c1[n] c2[n] codes (or both
// channels' Q8.8 precision words), copied under the fan-out lock (the
// arrays are reused by the next snapshot); the header carries sample_s and
// the sub-sample edge_x. No measurements: the stacker computes its own
// statistics, and skipping the measurement keeps the raw feed cheap next to
// the display path.
// TRLC-LINKS: REQ-DSO-163
func (s *Scope) rawFrame(since uint64) wire.RawFrame {
	if !s.v1Wire() {
		return wire.RawFrame{Header: wire.FrameReply{Seq: since, Unchanged: true, EdgeX: -1}, Unchanged: true}
	}
	off, vpc := s.scales().Scales()
	var rf wire.RawFrame
	s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		if f == nil || f.Seq == 0 || f.Seq == since {
			var seq uint64
			if f != nil {
				seq = f.Seq
			}
			rf = wire.RawFrame{Header: wire.FrameReply{Seq: seq, Unchanged: true, EdgeX: -1}, Unchanged: true}
			return
		}
		// clamp to the sample slices: Valid > len is an engine-invariant
		// violation, but a serve-path slice panic on one bad frame must never
		// crash the UI (frame-serve fuzz). C2 may be shorter than C1.
		n := min(f.Len(), len(f.Codes8(0)), len(f.Codes8(1)))
		if n <= 0 {
			rf = wire.RawFrame{Header: wire.FrameReply{Seq: f.Seq, Unchanged: true, EdgeX: -1}, Unchanged: true, Empty: true}
			return
		}
		hdr := wire.FrameReply{
			Seq: f.Seq, EdgeX: f.TriggerAt, Ptp: f.SourcePtp, TdivS: f.Snapshot.TdivS, WinCols: min(f.WinPoints, n),
			DisplayedS: f.Snapshot.DisplayedS, Interp: f.Flags.Has(sample.Interp), Norm: f.Snapshot.Norm,
			Trigd: f.Status.Triggered, Coherent: f.Status.Coherent, IsEnv: f.Flags.Has(sample.Envelope), PeakDetect: f.Content == sample.PeakPairs, Ordered: f.Flags.Has(sample.Ordered), CaptureSampleS: captureSampleS(f),
			Degraded: f.Status.Degraded, // a half capture's flag travels with the raw record it describes
			Cols:     n, ColSpanS: float64(n) * f.SampleS, SampleS: f.SampleS,
			EdgeFrac: -1, WinFrac: 1,
			Vpc1: vpc[0], Vpc2: vpc[1], Off1V: off[0], Off2V: off[1],
		}
		hdr.StreamSeq, hdr.WindowNs, hdr.GapNs = f.Stream.Seq, int64(math.Round(f.Stream.WindowS*1e9)), int64(math.Round(f.Stream.GapS*1e9))
		hdr.BandwidthHz, hdr.FilterGuard, hdr.Filter = f.Cond.BandwidthHz, f.Cond.Guard, f.Cond.Filter
		hdr.NoiseGainIdeal, hdr.PassbandHz = f.Cond.NoiseGain, f.Cond.PassbandHz
		hdr.Decimation, hdr.CaptureDepth, hdr.TriggerKind = uint32(f.AcqDecim), f.Depth, f.TriggerName() //nolint:gosec // G115: AcqDecim holds a fabric's 32-bit decimation word
		hdr.FullRate = f.Content == sample.RawFullRate
		if len(f.Precision(0)) == n && len(f.Precision(1)) == n {
			hdr.FractionBits = 8
			payload := make([]byte, 4*n)
			for ch, q := range [][]uint16{f.Precision(0), f.Precision(1)} {
				for i, v := range q {
					binary.LittleEndian.PutUint16(payload[2*(ch*n+i):], v)
				}
			}
			rf = wire.RawFrame{Header: hdr, Precision: true, Payload: payload}
			return
		}
		payload := make([]byte, 2*n)
		copy(payload[:n], f.Codes8(0)[:n])
		copy(payload[n:], f.Codes8(1)[:n])
		rf = wire.RawFrame{Header: hdr, Payload: payload}
	})
	return rf
}
