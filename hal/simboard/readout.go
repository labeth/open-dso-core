package simboard

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// peakSubSamples bounds the generator evaluations a peak-detect bucket
// takes its extremes from.
const peakSubSamples = 32

type readout struct{ b *Board }

// Limits implements hal.Readout.
// TRLC-LINKS: REQ-DSO-1001
func (r readout) Limits() hal.ReadLimits {
	return hal.ReadLimits{MaxPoints: r.b.profile.MaxDepth, MaxWindows: 16, MaxSegments: 1, ChannelsPerWindow: r.b.profile.Channels}
}

// Read samples each window of the stored record from the generator at the
// record's instants: a Span of 0 is the whole record. Full-rate windows deliver
// every sample, decimated ones pick, peak-detect ones take each bucket's
// extremes.
// TRLC-LINKS: REQ-DSO-1001
func (r readout) Read(ctx context.Context, req hal.ReadRequest, dst []*sample.Frame, yield func()) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b := r.b
	b.mu.Lock()
	rec := b.rec
	ok := rec.valid && (b.state == hal.Ready || b.state == hal.Idle)
	if ok {
		b.state = hal.Reading
	}
	b.mu.Unlock()
	if !ok {
		return hal.Refuse("readout", "no stored record: arm and wait for one first")
	}
	defer func() {
		b.mu.Lock()
		if b.state == hal.Reading {
			b.state = hal.Ready
		}
		b.mu.Unlock()
	}()
	if req.Record != (hal.RecordID{}) && req.Record != rec.status.Record {
		return hal.Refuse("record", "the board keeps only the newest record")
	}
	if req.Segments != [2]int{} && req.Segments != [2]int{0, 1} {
		return hal.Refuse("segments", b.profile.AbsentReason("segments"))
	}
	if len(dst) < len(req.Windows) {
		return fmt.Errorf("simboard: %d frames for %d windows", len(dst), len(req.Windows))
	}
	for i, w := range req.Windows {
		if i > 0 && yield != nil {
			yield()
		}
		if err := b.fill(w, rec, dst[i]); err != nil {
			return err
		}
	}
	return nil
}

// Export is one channel's whole record at its record rate.
// TRLC-LINKS: REQ-DSO-1001
func (r readout) Export(ctx context.Context, rec hal.RecordID, ch int, dst *sample.Trace) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b := r.b
	b.mu.Lock()
	stored := b.rec
	b.mu.Unlock()
	if !stored.valid || rec != stored.status.Record {
		return hal.Refuse("record", "the board keeps only the newest record")
	}
	g := geometryOf(stored.set.plan)
	f := &sample.Frame{}
	w := hal.Window{Role: hal.Main, Channels: []int{ch}, StartSample: g.first, Span: int64(g.n), Want: sample.RawDecimated}
	if stored.set.plan.AcqDecim == 1 {
		w.Want = sample.RawFullRate
	}
	if stored.set.plan.Content.Has(sample.PeakPairs) {
		w.Want = sample.PeakPairs
	}
	if err := b.fill(w, stored, f); err != nil {
		return err
	}
	*dst = f.Traces[0]
	return nil
}

// fill samples window w of record rec into f.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) fill(w hal.Window, rec record, f *sample.Frame) error {
	plan := rec.set.plan
	g := geometryOf(plan)
	want := w.Want
	if want == 0 {
		switch {
		case plan.Content.Has(sample.PeakPairs):
			want = sample.PeakPairs
		case plan.AcqDecim == 1:
			want = sample.RawFullRate
		default:
			want = sample.RawDecimated
		}
	}
	if !plan.Content.Has(want) {
		return hal.Refuse(want.String(), fmt.Sprintf("the record at this time/div holds %v", plan.Content))
	}
	start, span := g.first, int64(g.n)
	if w.Span > 0 {
		start, span = w.StartSample, w.Span
	}
	first, last := g.first, g.first+int64(g.n)
	if start < first || start+span > last {
		return hal.Refuse("readout window", fmt.Sprintf("samples %d…%d from the trigger lie outside the record (%d…%d)", start, start+span, first, last))
	}
	points := w.Points
	if points <= 0 {
		points = int(span)
	}
	if points > b.profile.MaxDepth {
		return hal.Refuse("readout window", fmt.Sprintf("%d points: at most %d a window", points, b.profile.MaxDepth))
	}
	if want == sample.RawFullRate && int64(points) < span {
		return hal.Refuse("raw-full-rate", "full-rate samples cannot be compressed: ask for decimated samples or peak pairs")
	}
	bucket := 1
	if int64(points) < span {
		bucket = int((span + int64(points) - 1) / int64(points))
	}
	n := int((span + int64(bucket) - 1) / int64(bucket))
	chans := w.Channels
	if len(chans) == 0 {
		for c, ch := range rec.set.channels {
			if ch.On {
				chans = append(chans, c)
			}
		}
	}
	content := want
	if want == sample.RawFullRate && bucket > 1 {
		content = sample.RawDecimated
	}
	tdiv := plan.Request.TimePerDivS
	*f = sample.Frame{
		Traces:    f.Traces[:0],
		Content:   content,
		Flags:     sample.Ordered,
		SampleS:   g.sampleS * float64(bucket),
		CaptureS:  plan.SampleS / float64(plan.AcqDecim),
		AcqDecim:  plan.AcqDecim,
		Depth:     plan.Depth,
		WinPoints: int(math.Round(float64(g.win) / float64(bucket))),
		TriggerAt: float64(-start) / float64(bucket),
		TrigKind:  sample.HardwareNode,
		TrigNode:  "edge",
		ContentID: uint64(rec.status.Record.Seq),
		Status:    rec.status,
		Snapshot:  sample.Settings{TdivS: tdiv, DisplayedS: tdiv, Norm: rec.set.trigger.Sweep != hal.Auto},
		Details:   f.Details[:0],
	}
	if len(rec.set.trigger.Stages) > 0 && rec.set.trigger.Stages[0].Node == hal.NodePulse {
		f.TrigNode = "pulse"
	}
	switch {
	case rec.free:
		f.TrigKind = sample.Untriggered
	case rec.status.Forced:
		f.TrigKind = sample.Forced
	}
	if want == sample.PeakPairs {
		// Each delivered code is half a bucket: a (min, max) pair per bucket.
		f.SampleS /= 2
		f.WinPoints *= 2
		f.TriggerAt *= 2
		f.Flags.Set(sample.Envelope, true)
	}
	t0 := rec.trigT + float64(start)*g.sampleS
	dt := g.sampleS * float64(bucket)
	src := -1
	if len(rec.set.trigger.Stages) > 0 {
		src = rec.set.trigger.Stages[0].Source.Index
	}
	for _, ch := range chans {
		if ch < 0 || ch >= len(rec.set.channels) {
			return hal.Refuse(fmt.Sprintf("channel %d", ch+1), "this scope has no such channel")
		}
		c := rec.set.channels[ch]
		v := b.viewOf(ch, c)
		f.Traces = append(f.Traces, sample.Trace{})
		tr := &f.Traces[len(f.Traces)-1]
		b.trace(tr, ch, v, t0, dt, n, want, plan)
		if ch == src {
			f.SourcePtp = ptp(tr)
		}
	}
	if len(f.Traces) == 0 {
		return errors.New("simboard: a window with no channel")
	}
	return nil
}

// coder turns a channel's tip volts into stored words.
type coder struct {
	f        sample.CodeFormat
	zero     float64
	perCode  float64
	offsetV  float64
	q        float64 // the word step of one code
	min, max float64
}

// coderOf is the word coding of setting c.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) coderOf(c hal.Channel) coder {
	f := b.profile.Format
	return coder{
		f: f, zero: float64(f.Mid), perCode: c.VoltsDiv / b.cpd, offsetV: c.OffsetV,
		q: float64(uint32(1) << f.Shift), min: float64(f.Min), max: float64(f.Max),
	}
}

// word is volts v as a stored word, on the code grid and clipped to the
// format's range.
// TRLC-LINKS: REQ-DSO-1001
func (c coder) word(v float64) uint32 {
	w := c.zero + (v+c.offsetV)/c.perCode
	w = math.Round(w/c.q) * c.q
	return uint32(min(max(w, c.min), c.max))
}

// trace fills t with n samples of view v from t0, dt apart.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) trace(t *sample.Trace, ch int, v view, t0, dt float64, n int, want sample.Content, plan hal.Plan) {
	cd := b.coderOf(v.ch)
	*t = sample.Trace{
		Ch: ch, Format: cd.f, Valid: n, CodesPerDiv: b.cpd,
		Scale: sample.Scale{Zero: cd.zero, PerCode: cd.perCode, OffsetV: cd.offsetV},
	}
	u16 := cd.f.Storage == sample.U16
	put := func(dst8 *[]uint8, dst16 *[]uint16, w uint32) {
		if u16 {
			*dst16 = append(*dst16, uint16(w)) //nolint:gosec // G115: clipped to the format's range, which fits its words
			return
		}
		*dst8 = append(*dst8, uint8(w)) //nolint:gosec // G115: clipped to the format's range, which fits its words
	}
	if want == sample.PeakPairs {
		t.Valid = 2 * n
		sub := min(max(int(math.Round(dt/plan.SampleS*float64(plan.AcqDecim))), 1), peakSubSamples)
		for i := range n {
			lo, hi := math.Inf(1), math.Inf(-1)
			for k := range sub {
				x := v.volts(t0 + float64(i)*dt + float64(k)*dt/float64(sub))
				lo, hi = min(lo, x), max(hi, x)
			}
			wl, wh := cd.word(lo), cd.word(hi)
			put(&t.Codes8, &t.Codes16, wl)
			put(&t.Codes8, &t.Codes16, wh)
			put(&t.Min8, &t.Min16, wl)
			put(&t.Max8, &t.Max16, wh)
		}
		return
	}
	vs := make([]float64, n)
	for i := range vs {
		vs[i] = v.volts(t0 + float64(i)*dt)
	}
	b.bandLimit(vs, v, t0, dt)
	for _, x := range vs {
		put(&t.Codes8, &t.Codes16, cd.word(x))
	}
}

// bandLimit passes vs, sampled dt apart from t0, through a single-pole
// low-pass at the channel's bandwidth (the scope's, or its limit), started
// early enough to have settled at the first sample.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) bandLimit(vs []float64, v view, t0, dt float64) {
	fc := b.profile.Bandwidth
	if lim := v.ch.BWLimit; lim > 0 && (fc <= 0 || lim < fc) {
		fc = lim
	}
	if fc <= 0 || v.ground {
		return
	}
	tau := 1 / (2 * math.Pi * fc)
	if dt > 5*tau {
		return // samples this far apart see the edges whole
	}
	a := 1 - math.Exp(-dt/tau)
	warm := min(int(math.Ceil(5*tau/dt)), 4096)
	y := v.volts(t0 - float64(warm)*dt)
	for i := warm - 1; i >= 1; i-- {
		y += a * (v.volts(t0-float64(i)*dt) - y)
	}
	for i, x := range vs {
		y += a * (x - y)
		vs[i] = y
	}
}

// ptp is a trace's peak-to-peak in codes.
// TRLC-LINKS: REQ-DSO-1001
func ptp(t *sample.Trace) int {
	n := t.Valid
	if n == 0 {
		return 0
	}
	lo, hi := t.Word(0), t.Word(0)
	for i := 1; i < n; i++ {
		w := t.Word(i)
		lo, hi = min(lo, w), max(hi, w)
	}
	return int((hi - lo) >> t.Format.Shift)
}
