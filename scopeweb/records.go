// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"context"
	"errors"
	"time"

	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// recordMin is the fewest samples a record served holds.
const recordMin = 1024

// Records takes whole sampled records: from the engine's record lease
// where it has one, else from the frames as they come.
// TRLC-LINKS: REQ-DSO-280
func (s *Scope) Records() (webserver.Records, bool) {
	return records{s}, s.cfg.Parts.RecordLease != nil || s.cfg.Board != nil
}

// records takes whole sampled records of every channel (the BUS Visual
// view: the live frames carry a screen's few thousand values, a record the
// memory's).
type records struct{ s *Scope }

// Take waits for a record captured after the call (the one in flight then
// may be of the screen before), sampled — no envelope — every sampleS or
// finer, leasing the engine's record samples meanwhile where it has the
// lease. A record that does not come is refused with why: the scope's
// state, not the server, keeps it.
// TRLC-LINKS: REQ-DSO-280
func (rs records) Take(ctx context.Context, want float64, wait time.Duration) (wire.Record, error) {
	s := rs.s
	lease := s.cfg.Parts.RecordLease
	var start uint64
	s.cfg.Frames.WithFrame(func(f *sample.Frame) {
		if f != nil {
			start = f.Seq
		}
	})
	deadline := time.Now().Add(wait)
	var rec wire.Record
	got := false
	renewed := time.Time{}
	for !got && time.Now().Before(deadline) && ctx.Err() == nil {
		if lease != nil && time.Since(renewed) > time.Second {
			lease(want, 3*time.Second)
			renewed = time.Now()
		}
		s.cfg.Frames.WithFrame(func(f *sample.Frame) {
			if f == nil || f.Seq <= start+1 || f.Flags.Has(sample.Envelope) || f.Content == sample.PeakPairs || !(f.SampleS > 0) || f.SampleS > want*1.01 {
				return
			}
			rec, got = s.recordOf(f)
		})
		if !got {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !got {
		if _, running, _ := s.scales().Position(); !running {
			return wire.Record{}, errors.New("the scope is stopped: it captures no record (Run it)")
		}
		return wire.Record{}, errors.New("no record sampled that finely came in time: a waiting or streaming scope captures none, and the memory bounds the record (a slower timebase at this rate does not fit)")
	}
	if s.v1Wire() {
		rec.Off, rec.Vpc = s.scales().Scales()
	}
	return rec, nil
}

// recordOf copies frame f's record: a two-channel 8-bit scope's two
// channels as v1 carries them, any other scope's every channel on the
// 8-bit view with its own scale. False when it holds too few samples.
// TRLC-LINKS: REQ-DSO-280
func (s *Scope) recordOf(f *sample.Frame) (wire.Record, bool) {
	rec := wire.Record{Seq: f.Seq, SampleS: f.SampleS, TdivS: f.Snapshot.TdivS}
	if s.v1Wire() {
		n := min(f.Len(), len(f.Codes8(0)), len(f.Codes8(1)))
		if n < recordMin {
			return rec, false
		}
		rec.C1 = append([]byte(nil), f.Codes8(0)[:n]...)
		rec.C2 = append([]byte(nil), f.Codes8(1)[:n]...)
		return rec, true
	}
	if len(f.Traces) == 0 || f.Len() < recordMin {
		return rec, false
	}
	for i := range f.Traces {
		t := &f.Traces[i]
		v := t.View8()
		if len(v) < recordMin {
			return rec, false
		}
		vpc, off := view8Scale(t)
		codes := v
		if t.Format.Storage != sample.U16 {
			codes = append([]byte(nil), v...) // View8 of 8-bit codes is the frame's own memory
		}
		rec.Chans = append(rec.Chans, wire.RecordChan{Ch: t.Ch, Codes: codes, Vpc: vpc, Off: off})
	}
	return rec, true
}
