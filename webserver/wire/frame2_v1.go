// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

// FromReply fills f from a v1 reply: the two-channel 8-bit frame a v1 scope
// builds, at 25 codes a division about code 128 (volts = (code − 128)·vpc −
// off). Its -1 margins become each trace's head and tail, its envelope the
// min and max segments, its Q8.8 precision words u16 codes with 8 fraction
// bits, and its view window the view segments. A scope that builds v1
// replies serves v2 with it until it fills f from its own frames.
// TRLC-LINKS: REQ-DSO-163
func (f *Frame) FromReply(rep FrameReply) {
	f.Reset()
	f.Header = FrameHeader{
		Seq: rep.Seq, Unchanged: rep.Unchanged,
		SampleS: rep.SampleS, CaptureS: rep.CaptureSampleS, AcqDecim: int(rep.Decimation), TriggerAt: rep.EdgeX,
		TrigKind: rep.TriggerKind, TdivS: rep.TdivS, DisplayedS: rep.DisplayedS, Cols: rep.Cols, ColSpanS: rep.ColSpanS,
		DtS: rep.DtS, Depth: rep.Depth, CaptureDep: rep.CaptureDepth, WinCols: rep.WinCols, EdgeX: rep.EdgeX,
		EdgeFrac: rep.EdgeFrac, WinFrac: rep.WinFrac, Held: rep.Held, StreamSeq: rep.StreamSeq,
		WindowNs: rep.WindowNs, GapNs: rep.GapNs, MatchEndS: rep.MatchEndS, Ptp: rep.Ptp, Interp: rep.Interp,
		Norm: rep.Norm, Trigd: rep.Trigd, Coherent: rep.Coherent, Degraded: rep.Degraded,
		Filter: rep.Filter, FilterGuard: rep.FilterGuard, BandwidthHz: rep.BandwidthHz, PassbandHz: rep.PassbandHz,
		NoiseGainIdeal: rep.NoiseGainIdeal, MeasSeq: rep.MeasSeq, EdgeRead: rep.EdgeRead,
	}
	switch {
	case rep.PeakDetect || rep.IsEnv:
		f.Header.Content = "peak-pairs"
	case rep.FullRate:
		f.Header.Content = "raw-full-rate"
	}
	if rep.Ordered {
		f.Header.Flags = []string{"ordered"}
	}
	if rep.Unchanged {
		return
	}
	precision := rep.FractionBits == 8 && len(rep.Q1) > 0 && len(rep.Q1) == len(rep.C1) && len(rep.Q2) == len(rep.C2)
	type chv1 struct {
		c, emin, emax, view []int16
		q                   []uint16
		vpc, off            float64
		clip                bool
	}
	chans := [2]chv1{
		{rep.C1, rep.E1Min, rep.E1Max, rep.VC1, rep.Q1, rep.Vpc1, rep.Off1V, rep.Clip1},
		{rep.C2, rep.E2Min, rep.E2Max, rep.VC2, rep.Q2, rep.Vpc2, rep.Off2V, rep.Clip2},
	}
	for ch, c := range chans {
		if len(c.c) == 0 && len(c.emin) == 0 {
			continue
		}
		t := FrameTrace{TraceHeader: TraceHeader{
			Ch: ch, Fmt: CodeFormat{Bits: 8, Storage: "u8", Mid: 128, Max: 255},
			Scale: TraceScale{Zero: 128, PerCodeV: c.vpc, OffV: c.off, CodesPerDiv: 25}, Clip: c.clip,
		}}
		if ch == 0 {
			t.Meas = rep.M1
		} else {
			t.Meas = rep.M2
		}
		head, tail := margins(c.c)
		switch {
		case rep.IsEnv:
			t.Min.U8, t.Max.U8 = codes8(c.emin), codes8(c.emax)
		case precision:
			t.Fmt = CodeFormat{Bits: 8, Storage: "u16", Frac: 8, Mid: 128 << 8, Max: 255 << 8}
			t.Scale = TraceScale{Zero: 128 << 8, PerCodeV: c.vpc / 256, OffV: c.off, CodesPerDiv: 25 << 8}
			t.Codes.U16 = append([]uint16(nil), c.q[head:len(c.q)-tail]...)
			t.Head, t.Tail = head, tail
		default:
			t.Codes.U8 = codes8(c.c[head : len(c.c)-tail])
			t.Head, t.Tail = head, tail
		}
		if !rep.IsEnv && rep.ViewN > 0 && len(c.view) == rep.ViewN && !precision {
			t.View.U8 = codes8(c.view)
		}
		f.Traces = append(f.Traces, t)
	}
	if len(f.Traces) == 2 && (len(f.Traces[0].View.U8) == 0) == (len(f.Traces[1].View.U8) == 0) && len(f.Traces[0].View.U8) > 0 {
		f.Header.ViewA, f.Header.ViewB, f.Header.ViewN = rep.ViewA, rep.ViewB, rep.ViewN
	} else {
		for i := range f.Traces {
			f.Traces[i].View = Samples{}
		}
	}
}

// margins counts c's contiguous -1 head and tail runs.
// TRLC-LINKS: REQ-DSO-163
func margins(c []int16) (head, tail int) {
	for head < len(c) && c[head] < 0 {
		head++
	}
	for tail < len(c)-head && c[len(c)-1-tail] < 0 {
		tail++
	}
	return head, tail
}

// codes8 is c as bytes (display codes, 0..255).
// TRLC-LINKS: REQ-DSO-163
func codes8(c []int16) []uint8 {
	out := make([]uint8, len(c))
	for i, v := range c {
		out[i] = uint8(v & 0xff)
	}
	return out
}
