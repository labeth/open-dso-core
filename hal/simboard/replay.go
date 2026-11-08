package simboard

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// replayMaxBytes bounds a replayed record's upload.
const replayMaxBytes = 64 << 20

// replay is a recorded record the board plays back on the channels it
// holds: at the record's own sample interval the board's record is the
// captured one, word for word, with the capture's code format, scale and
// trigger; at any other timebase each channel is the captured signal's
// volts, held between its samples and repeating.
type replay struct {
	f       *sample.Frame
	traces  map[int]*sample.Trace // by channel
	trig    int                   // the trigger's sample in the record
	periodS float64               // the record's length, the signal's repetition
}

// Replay plays back record f (a scope's raw record: its traces' codes,
// format and scale, its sample interval and trigger) on the channels it
// holds, until StopReplay. A record in another code format than the
// profile's is refused: its words would not be this scope's.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) Replay(f *sample.Frame) error {
	if f == nil || len(f.Traces) == 0 || !(f.SampleS > 0) {
		return hal.Refuse("replay", "a record with traces and a sample interval")
	}
	r := &replay{f: f, traces: map[int]*sample.Trace{}, trig: int(math.Round(f.TriggerAt))}
	n := 0
	for i := range f.Traces {
		t := &f.Traces[i]
		if t.Ch < 0 || t.Ch >= b.profile.Channels {
			return hal.Refuse("replay", fmt.Sprintf("the record's channel %d is not on this scope", t.Ch+1))
		}
		if t.Format != b.profile.Format {
			nt, ok := codesWithPrecision(t, b.profile.Format)
			if !ok {
				return hal.Refuse("replay", fmt.Sprintf("the record's C%d words are %d-bit %v with %d fraction bits; this scope stores %d-bit %v",
					t.Ch+1, t.Format.Bits, t.Format.Storage, t.Format.Frac, b.profile.Format.Bits, b.profile.Format.Storage))
			}
			*t = nt
			f.Flags.Set(sample.Precision, true)
		}
		if t.Valid <= 0 || !(t.Scale.PerCode != 0) {
			return hal.Refuse("replay", fmt.Sprintf("the record's C%d has no samples or no scale", t.Ch+1))
		}
		r.traces[t.Ch] = t
		n = max(n, t.Valid)
	}
	r.periodS = float64(n) * f.SampleS
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rep = r
	return nil
}

// codesWithPrecision is trace t, whose words are the codes of format conv
// with fraction bits below them (a scope's precision words beside its
// 8-bit codes, as wire v2 carries them: the code format with the fraction
// bits added, its mid, rails and scale stated in the finer words), as the
// scope held it: its codes rounded to conv's (a half rounds up, as the
// scope rounds its Q8.8 words), the words kept beside them as its
// precision words, its scale in codes again. ok is false for words that
// are not conv's codes so refined, or a format whose codes are not 8-bit
// words (precision words sit beside 8-bit codes).
// TRLC-LINKS: REQ-DSO-1001
func codesWithPrecision(t *sample.Trace, conv sample.CodeFormat) (sample.Trace, bool) {
	tf := t.Format
	if conv.Storage != sample.U8 || tf.Storage != sample.U16 || tf.Bits != conv.Bits || tf.Shift != conv.Shift || tf.Frac <= conv.Frac {
		return sample.Trace{}, false
	}
	fr := tf.Frac - conv.Frac
	if tf.Mid != conv.Mid<<fr || tf.Min != conv.Min<<fr || tf.Max != conv.Max<<fr || tf.Validate() != nil {
		return sample.Trace{}, false
	}
	n := min(t.Valid, len(t.Codes16))
	k := float64(uint32(1) << fr)
	out := sample.Trace{
		Ch: t.Ch, Format: conv, Valid: n,
		Codes8: make([]uint8, n), Precision: slices.Clone(t.Codes16[:n]), PrecisionFrac: fr,
		Scale:       sample.Scale{Zero: t.Scale.Zero / k, PerCode: t.Scale.PerCode * k, OffsetV: t.Scale.OffsetV},
		CodesPerDiv: t.CodesPerDiv / k,
	}
	half := uint32(1) << (fr - 1)
	for i, w := range out.Precision {
		out.Codes8[i] = uint8(min((uint32(w)+half)>>fr, conv.Max)) //nolint:gosec // G115: clamped to conv's 8-bit rail
	}
	return out, true
}

// StopReplay returns every channel to the generator.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) StopReplay() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rep = nil
}

// verbatim reports whether plan p takes the replayed record word for
// word: at the record's own time/div (to a part in a million) the board
// holds what the scope held there, whatever record it plans itself (a
// scope's peak-detect display readout of a deeper record among them);
// at another, a plan whose sample interval is the record's takes a
// record of samples. A peak-detect plan takes only a record of pairs.
// TRLC-LINKS: REQ-DSO-1001
func (r *replay) verbatim(p hal.Plan) bool {
	if r == nil {
		return false
	}
	pairs := r.f.Content == sample.PeakPairs
	if p.Content.Has(sample.PeakPairs) && !pairs {
		return false
	}
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-6*b }
	if td := r.f.Snapshot.TdivS; td > 0 && near(p.Request.TimePerDivS, td) {
		return true
	}
	return !pairs && near(p.SampleS, r.f.SampleS)
}

// volts is channel ch's captured tip voltage at signal time t, held
// between samples and repeating every record; ok false for a channel the
// record does not hold.
// TRLC-LINKS: REQ-DSO-1001
func (r *replay) volts(ch int, t float64) (float64, bool) {
	if r == nil || r.traces[ch] == nil {
		return 0, false
	}
	tr := r.traces[ch]
	ph := math.Mod(t, r.periodS)
	if ph < 0 {
		ph += r.periodS
	}
	i := min(int(ph/r.f.SampleS), tr.Valid-1)
	if len(tr.Precision) > i && tr.PrecisionFrac > 0 {
		return tr.Scale.Volts(math.Ldexp(float64(tr.Precision[i]), -int(tr.PrecisionFrac))), true
	}
	return tr.Scale.Volts(float64(tr.Word(i))), true
}

// fillReplay fills f with window w of the replayed record r, for the
// channels chans: the whole record for a window of no span, else the span
// around the record's trigger; ok false when no channel is replayed.
// TRLC-LINKS: REQ-DSO-1001
func (r *replay) fill(w hal.Window, rec record, chans []int, divs int, f *sample.Frame) (bool, error) {
	if !slices.ContainsFunc(chans, func(c int) bool { return r.traces[c] != nil }) {
		return false, nil
	}
	src := r.f
	n := 0
	for _, t := range r.traces {
		n = max(n, t.Valid)
	}
	start, span := -int64(r.trig), int64(n)
	if w.Span > 0 {
		start, span = w.StartSample, w.Span
	}
	lo := int64(r.trig) + start
	if lo < 0 || lo+span > int64(n) {
		return true, hal.Refuse("readout window", fmt.Sprintf("samples %d…%d from the trigger lie outside the replayed record (%d…%d)",
			start, start+span, -r.trig, n-r.trig))
	}
	if w.Points > 0 && int64(w.Points) < span {
		return true, hal.Refuse("readout window", "a replayed record is read word for word, not compressed")
	}
	plan := rec.set.plan
	*f = sample.Frame{
		Traces: f.Traces[:0], Content: src.Content, Flags: src.Flags,
		SampleS: src.SampleS, CaptureS: src.CaptureS, AcqDecim: src.AcqDecim,
		Depth: max(src.Depth, n), WinPoints: src.WinPoints,
		TriggerAt: src.TriggerAt - float64(lo), TrigKind: src.TrigKind, TrigNode: src.TrigNode,
		ContentID: uint64(rec.status.Record.Seq), Status: rec.status,
		Snapshot: sample.Settings{TdivS: plan.Request.TimePerDivS, DisplayedS: plan.Request.TimePerDivS, Norm: rec.set.trigger.Sweep != hal.Auto},
		Details:  f.Details[:0],
	}
	if f.Content == 0 {
		f.Content = sample.RawFullRate
	}
	if f.CaptureS == 0 {
		f.CaptureS = src.SampleS
	}
	if f.WinPoints == 0 {
		f.WinPoints = int(math.Round(plan.Request.TimePerDivS * float64(divs) / src.SampleS))
	}
	for _, ch := range chans {
		t := r.traces[ch]
		if t == nil {
			return true, hal.Refuse(fmt.Sprintf("C%d", ch+1), "the replayed record does not hold this channel")
		}
		out := sample.Trace{Ch: ch, Format: t.Format, Scale: t.Scale, CodesPerDiv: t.CodesPerDiv, Valid: int(span)}
		a, z := int(lo), int(lo+span)
		if t.Format.Storage == sample.U16 {
			out.Codes16 = slices.Clone(t.Codes16[a:z])
		} else {
			out.Codes8 = slices.Clone(t.Codes8[a:z])
		}
		if len(t.Precision) >= z {
			out.Precision, out.PrecisionFrac = slices.Clone(t.Precision[a:z]), t.PrecisionFrac
		}
		f.Traces = append(f.Traces, out)
	}
	return true, nil
}

// hReplay plays back the raw record in the body (a v2 raw frame as a
// scope's /api/frame.bin?v=2&raw=1&full=1 serves it); DELETE returns the
// inputs to the generator.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) hReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		b.StopReplay()
		reply(w, map[string]any{"ok": true})
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, replayMaxBytes))
	if err != nil {
		fail(w, err)
		return
	}
	f, err := FrameOfWire(body)
	if err == nil {
		err = b.Replay(f)
	}
	if err != nil {
		fail(w, err)
		return
	}
	chans := make([]string, 0, len(f.Traces))
	for _, t := range f.Traces {
		chans = append(chans, fmt.Sprintf("C%d", t.Ch+1))
	}
	reply(w, map[string]any{"ok": true, "channels": chans, "sample_s": f.SampleS, "samples": f.Len()})
}

// FrameOfWire is a v2 raw frame as the record it carries: its traces'
// codes, code format and scale, its sample interval, trigger and timebase.
// TRLC-LINKS: REQ-DSO-1001
func FrameOfWire(b []byte) (*sample.Frame, error) {
	wf, err := wire.DecodeFrame2(b)
	if err != nil {
		return nil, err
	}
	h := wf.Header
	var content sample.Content
	_ = content.UnmarshalText([]byte(h.Content)) // unnamed: a full-rate record
	f := &sample.Frame{
		Content: content, SampleS: h.SampleS, CaptureS: h.CaptureS, AcqDecim: h.AcqDecim, TriggerAt: h.TriggerAt,
		Depth: h.Depth, WinPoints: h.WinCols, Seq: h.Seq, Snapshot: sample.Settings{TdivS: h.TdivS, DisplayedS: h.DisplayedS},
	}
	n := 0
	for _, t := range wf.Traces {
		var st sample.StorageKind
		_ = st.UnmarshalText([]byte(t.Fmt.Storage)) // an unknown storage leaves the trace without codes
		tr := sample.Trace{
			Ch: t.Ch, Format: sample.CodeFormat{Bits: t.Fmt.Bits, Storage: st, Shift: t.Fmt.Shift, Frac: t.Fmt.Frac, Min: t.Fmt.Min, Max: t.Fmt.Max, Mid: t.Fmt.Mid},
			Scale:       sample.Scale{Zero: t.Scale.Zero, PerCode: t.Scale.PerCodeV, OffsetV: t.Scale.OffV},
			CodesPerDiv: t.Scale.CodesPerDiv, Codes8: t.Codes.U8, Codes16: t.Codes.U16, Valid: t.Codes.Len(),
		}
		if tr.Valid > 0 && (n == 0 || tr.Valid < n) {
			n = tr.Valid
		}
		f.Traces = append(f.Traces, tr)
	}
	if n == 0 {
		return nil, errors.New("the raw frame holds no samples")
	}
	f.SetLen(n)
	return f, nil
}
