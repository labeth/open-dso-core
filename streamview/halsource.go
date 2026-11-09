// ENGMODEL-OWNER-UNIT: FU-DSO-STREAMVIEW

package streamview

import (
	"context"

	"github.com/labeth/open-dso-core/decodedlines"
	"github.com/labeth/open-dso-core/engine"
)

// HALSource is the HAL cycle's decoded stream (the board's stream of codes
// decoded in software) as a controller follows it. Its stop shows no
// record: the board keeps no history ring.
type HALSource struct{ C *engine.HALCycle }

var _ Source = HALSource{}

// BeginDecodedStream starts the cycle's decoded stream.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) BeginDecodedStream(ctx context.Context, p engine.SerialParams, trig engine.StreamTrigger, value int) (ID, error) {
	epoch, err := s.C.StartDecoded(ctx, p, trig, value)
	return ID{Epoch: epoch, Record: 1}, err
}

// ReadDecodedBatchInto reads the stream's queued events.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) ReadDecodedBatchInto(_ context.Context, id ID, dst []decodedlines.Event) (int, error) {
	return s.C.ReadDecoded(id.Epoch, dst)
}

// DecodedCaptureStatus reports a transcript that ended on the value as
// triggered, with the value's frame as its span.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) DecodedCaptureStatus(_ context.Context, id ID) (Status, error) {
	hit, first, last, err := s.C.DecodedStatus(id.Epoch)
	if !hit {
		return Status{}, err
	}
	return Status{Triggered: true, Frozen: true, Ready: true, HasSampleTimeline: true, SampleFirst: first, SampleLast: last}, nil
}

// FreezeDecodedStream stops the stream; no record goes to the display.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) FreezeDecodedStream(ctx context.Context, id ID) (Status, bool, error) {
	hit, first, last, err := s.C.FreezeDecoded(ctx, id.Epoch)
	if err != nil || !hit {
		return Status{}, false, err
	}
	return Status{Triggered: true, Frozen: true, Ready: true, HasSampleTimeline: true, SampleFirst: first, SampleLast: last}, false, nil
}

// EndDecodedCapture tears the stream down.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) EndDecodedCapture(ctx context.Context, id ID) error {
	return s.C.EndDecoded(ctx, id.Epoch)
}

// ReadDecodedLines is event mode: the controller builds the lines.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) ReadDecodedLines(context.Context, ID, int, bool) (decodedlines.Batch, bool, error) {
	return decodedlines.Batch{}, false, nil
}

// CheckDecodedStream refuses a bus the board's stream cannot carry.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) CheckDecodedStream(p engine.SerialParams) error { return s.C.CheckDecoded(p) }

// DecodedStreamMode says how the stream runs, for the displays.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s HALSource) DecodedStreamMode() string { return s.C.DecodedMode() }
