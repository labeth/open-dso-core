// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/decodedlines"
	"github.com/labeth/open-dso-core/hal"
)

// The decoded stream on a board with a gap-free stream of sample codes
// (hal.Streamer, PayloadCodes), as the SDS1000CML+ decodes its stream
// image's continuous stream (ADR-STREAM-SOFT-DECODE): while a stream runs,
// the owner takes the acquisition for the board's stream at its fastest
// stream rate, puts the bus's lines on the 8-bit view and decodes them in
// software as they arrive (decode.StreamDecoder); its frames become decoded
// events a controller (streamview.Controller, over HALDecodedSource) folds
// into the transcript. A stop on a value ends the transcript at the frame
// that holds it: on a gap-free decoded stream software may be the trigger.
// The board keeps no history ring, so the stop shows no record. A stop on a
// decode error runs until stopped by hand, as the SDS1000CML+'s software
// streams do. Starting a stream stops the run, as on the SDS1000CML+; the
// UI model runs it again when its stream ends.

// halDecodedEventsMax bounds the queued events, as the SDS1000CML+'s FPGA
// decoded events are (5 MiB of 40-byte events): a reader that stops reading
// ends the transcript rather than the memory.
const halDecodedEventsMax = 131072

// halDecodedPushMax bounds the samples a line one decode takes: a decode's
// working memory grows with its window.
const halDecodedPushMax = 1 << 18

// halDecodedMinSamples is the samples the shortest feature of a bus needs
// for a decode of the stream: the SDS1000CML+'s gap-free rule
// (decode.StreamBucketLog's three and a half buckets).
const halDecodedMinSamples = 3.5

// halDecodedBudget is the most samples a second (all lines) the scope
// decodes beside its display: the SDS1000CML+'s bench budget for its
// software streams.
const halDecodedBudget = 4e6

// halDecodedStartWait bounds a start's wait for the owner.
const halDecodedStartWait = 20 * time.Second

// halDecodedEpochBase marks the event epochs of the cycle's streams.
const halDecodedEpochBase = 0xD0000000

// halDecoded is the cycle's decoded stream.
type halDecoded struct {
	mu      sync.Mutex
	pending *decodedStart // a start a controller waits for
	epoch   uint32        // the stream a controller follows; 0 none
	epochs  uint32
	stopReq bool          // a controller asked the stream to stop
	stopped chan struct{} // closed once the owner stopped the stream asked to stop
	events  []decodedlines.Event
	evHead  int
	seq     uint32
	done    bool // the transcript ended (a stop, a value read, a failure)
	hit     bool // it ended on the value
	first   uint64
	last    uint64 // the stop frame's span, in 2 ns ordinals
	err     error
	mode    string

	// owner only
	st       hal.Streamer
	active   bool // a stream runs (or waits to start again after a gap)
	running  bool // the board's stream is started
	gap      bool // the next block follows a gap
	retryAt  time.Time
	p        SerialParams
	decoder  SerialDecoder
	name     string
	trig     StreamTrigger
	byHand   bool // asked to stop on a decode error: it stops by hand
	value    int
	mask     int
	twoLine  bool
	chans    []int // the channels the board streams, in its blocks' order
	sampleS  float64
	feature  float64
	quiet    float64
	longest  float64
	dec      *decode.StreamDecoder
	base     uint64 // the stream sample the decoder's index 0 is
	next     uint64 // the next stream sample
	ordFrom  uint64 // the stream sample the sample time last changed at
	ordAt    uint64 // its 2 ns ordinal
	seqNext  uint64 // the board's sequence number of the next block
	haveSeq  bool   // seqNext is known (a block since the board's stream started)
	pushN    int
	pendA    []uint8
	pendB    []uint8
	bLo, bHi uint8
	bSeen    bool
}

// decodedStart is a controller's start waiting for the owner.
type decodedStart struct {
	p     SerialParams
	trig  StreamTrigger
	value int
	epoch uint32
	done  chan error
}

// decodedStreamer is the board's stream of codes, or why there is none.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedStreamer() (hal.Streamer, error) {
	st, ok := c.b.Streamer()
	if !ok {
		return nil, hal.Refuse("decoded stream", c.b.Profile().AbsentReason("streamer"))
	}
	if !slices.Contains(st.Kinds(), hal.PayloadCodes) {
		return nil, hal.Refuse("decoded stream", "the board's stream carries no sample codes")
	}
	return st, nil
}

// DecodesStreams reports a board whose stream of codes the cycle decodes.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) DecodesStreams() bool {
	_, err := c.decodedStreamer()
	return err == nil
}

// decodedName is protocol proto's decode package name when the cycle
// decodes its stream: the software-decoded protocols and MIDI.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func decodedName(proto int) (string, bool) {
	if name, ok := SoftStreamName(proto); ok {
		return name, true
	}
	if proto == SerMIDI {
		return "midi", true
	}
	return "", false
}

// CheckDecoded refuses a decoded stream of p's bus the board's stream
// cannot carry, before any setting of the stream is known (the threshold
// comes from a decode of the screen): no stream of codes, a protocol the
// scope does not decode from a stream, a line that is off, a bus faster
// than the board's fastest stream resolves. ANY.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) CheckDecoded(p SerialParams) error {
	if _, err := c.decodedStreamer(); err != nil {
		return err
	}
	d, ok := LookupSerialDecoder(p.Proto)
	name, named := decodedName(p.Proto)
	if !ok || !named {
		return hal.Refuse("decoded stream", serialName(p.Proto, d)+" is not decoded from a stream on this scope")
	}
	feature, _, _, known := decode.StreamTiming(name, float64(p.Baud))
	if !known {
		return hal.Refuse("decoded stream", "no stream timing for "+d.Name)
	}
	if name == SoftOneWire && p.Overdrive {
		feature /= 10
	}
	r := c.inst.State().Request()
	if !slices.Contains(r.Channels, p.ChA) || (ClockedTwoLine(p.Proto) && !slices.Contains(r.Channels, p.ChB)) {
		return hal.Refuse("decoded stream", fmt.Sprintf("the bus's lines are not all on (C%d, C%d)", p.ChA+1, p.ChB+1))
	}
	_, plan, err := c.streamPlanOf(r)
	if err != nil {
		return err
	}
	return c.decodedRate(d.Name, p.Baud, plan.SampleS, feature, ClockedTwoLine(p.Proto))
}

// serialName is a protocol's name for a refusal.
// TRLC-LINKS: REQ-DSO-018
func serialName(proto int, d SerialDecoder) string {
	if d.Name != "" {
		return d.Name
	}
	return fmt.Sprintf("protocol %d", proto)
}

// StartDecoded starts a decoded stream of p's bus that ends as trig and
// value say, and returns its event epoch once the owner started it (or
// why it did not). A stream already running ends first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) StartDecoded(ctx context.Context, p SerialParams, trig StreamTrigger, value int) (uint32, error) {
	if _, err := c.decodedStreamer(); err != nil {
		return 0, err
	}
	d, ok := LookupSerialDecoder(p.Proto)
	name, named := decodedName(p.Proto)
	if !ok || !named {
		return 0, fmt.Errorf("no software stream for protocol %d", p.Proto)
	}
	if !p.HaveThr {
		return 0, fmt.Errorf("%s streams need the decode's threshold", d.Name)
	}
	if _, _, _, known := decode.StreamTiming(name, float64(p.Baud)); !known {
		return 0, fmt.Errorf("no stream timing for %s", d.Name)
	}
	s := &c.dec
	s.mu.Lock()
	s.epochs++
	st := &decodedStart{p: p, trig: trig, value: value, epoch: halDecodedEpochBase | s.epochs&0x0fffffff, done: make(chan error, 1)}
	if s.pending != nil {
		s.pending.done <- errors.New("a newer stream started")
	}
	s.pending = st
	s.mu.Unlock()
	wait, cancel := context.WithTimeout(ctx, halDecodedStartWait)
	defer cancel()
	select {
	case err := <-st.done:
		return st.epoch, err
	case <-wait.Done():
		s.mu.Lock()
		if s.pending == st {
			s.pending = nil
		}
		s.mu.Unlock()
		return 0, errors.New("the stream did not start in time")
	}
}

// ReadDecoded copies stream epoch's queued events into dst.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) ReadDecoded(epoch uint32, dst []decodedlines.Event) (int, error) {
	s := &c.dec
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || epoch == 0 {
		return 0, errors.New("decoded capture identity is no longer active")
	}
	n := copy(dst, s.events[s.evHead:])
	if s.evHead += n; s.evHead == len(s.events) {
		s.events, s.evHead = s.events[:0], 0
	}
	if n == 0 && s.err != nil && !s.done {
		return 0, s.err
	}
	return n, nil
}

// DecodedStatus reports stream epoch's end: hit once its transcript ended
// on the value (first and last its stop frame's span), err when it failed.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) DecodedStatus(epoch uint32) (hit bool, first, last uint64, err error) {
	s := &c.dec
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch || epoch == 0 {
		return false, 0, 0, errors.New("decoded capture identity is no longer active")
	}
	if !s.done {
		return false, 0, 0, nil
	}
	if s.hit {
		return true, s.first, s.last, nil
	}
	return false, 0, 0, s.err
}

// FreezeDecoded stops stream epoch's acquisition: the owner decodes what
// it holds and stops the board's stream. It reports whether the
// transcript ended on the value, and that frame's span.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) FreezeDecoded(ctx context.Context, epoch uint32) (hit bool, first, last uint64, err error) {
	s := &c.dec
	s.mu.Lock()
	if s.epoch != epoch || epoch == 0 {
		s.mu.Unlock()
		return false, 0, 0, errors.New("decoded capture identity is no longer active")
	}
	if s.stopped == nil {
		s.stopped = make(chan struct{})
	}
	s.stopReq = true
	stopped := s.stopped
	s.mu.Unlock()
	select {
	case <-stopped:
	case <-ctx.Done():
		return false, 0, 0, ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hit, s.first, s.last, nil
}

// EndDecoded tears stream epoch down; the acquisition stays stopped until
// RUN.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) EndDecoded(ctx context.Context, epoch uint32) error {
	s := &c.dec
	s.mu.Lock()
	running := s.epoch == epoch && epoch != 0 && !s.done
	s.mu.Unlock()
	if running {
		if _, _, _, err := c.FreezeDecoded(ctx, epoch); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.epoch != epoch {
		return errors.New("decoded capture identity is no longer active")
	}
	s.epoch, s.events, s.evHead = 0, nil, 0
	return nil
}

// DecodedMode says how the decoded stream runs, for the displays.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) DecodedMode() string {
	s := &c.dec
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// decodedWanted reports a start waiting for the owner, for the wait's
// watch.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedWanted() bool {
	s := &c.dec
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending != nil || (s.stopReq && s.stopped != nil)
}

// decodedStep runs the decoded stream one step on the owner; false when
// none runs and the pass goes on.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedStep(ctx context.Context) bool {
	s := &c.dec
	s.mu.Lock()
	st, stop := s.pending, s.stopReq
	if len(c.cur.Channels) > 0 {
		s.pending = nil
	} else {
		st = nil // nothing committed yet: the pass commits the state first
	}
	s.mu.Unlock()
	if st != nil {
		if s.active {
			c.decodedEnd(nil)
		}
		err := c.decodedBegin(st)
		st.done <- err
		if err != nil {
			return false
		}
	}
	if stop {
		if s.active {
			c.decodedPush(true)
			c.decodedEnd(nil)
		}
		s.mu.Lock()
		s.stopReq = false
		if s.stopped != nil {
			close(s.stopped)
			s.stopped = nil
		}
		s.mu.Unlock()
		return false
	}
	if !s.active {
		return false
	}
	if !s.running {
		if time.Now().Before(s.retryAt) {
			c.idle(ctx)
			return true
		}
		if err := c.decodedRun(); err != nil {
			c.cfg.Logf("decoded stream: %v", err)
			c.decodedEnd(err)
			return false
		}
	}
	c.decodedDrain(ctx)
	return true
}

// decodedBegin checks a start against the board's stream and opens the
// stream: the run stops, the transcript's events start afresh.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedBegin(st *decodedStart) error {
	s := &c.dec
	stream, err := c.decodedStreamer()
	if err != nil {
		return err
	}
	if c.snd.running {
		c.soundStop()
	}
	p := st.p
	d, _ := LookupSerialDecoder(p.Proto)
	name, _ := decodedName(p.Proto)
	feature, longest, quiet, _ := decode.StreamTiming(name, float64(p.Baud))
	if name == SoftOneWire && p.Overdrive {
		feature, longest, quiet = feature/10, longest/10, quiet/10
	}
	r, plan, err := c.streamPlan()
	if err != nil {
		return err
	}
	if !slices.Contains(r.Channels, p.ChA) || (ClockedTwoLine(p.Proto) && !slices.Contains(r.Channels, p.ChB)) {
		return hal.Refuse("decoded stream", fmt.Sprintf("the bus's lines are not all on (C%d, C%d)", p.ChA+1, p.ChB+1))
	}
	if err := c.decodedRate(d.Name, p.Baud, plan.SampleS, feature, ClockedTwoLine(p.Proto)); err != nil {
		return err
	}
	c.stopArmed()
	c.stopRun(false) // streaming is not RUN
	s.mu.Lock()
	s.epoch, s.events, s.evHead, s.seq = st.epoch, s.events[:0], 0, 0
	s.done, s.hit, s.first, s.last, s.err = false, false, 0, 0, nil
	s.mu.Unlock()
	s.st, s.p, s.decoder, s.name, s.trig = stream, p, d, name, st.trig
	s.mask = UnitMask(p)
	s.value = st.value & s.mask
	s.byHand = s.trig == StreamOnError
	if s.byHand {
		s.trig = StreamManual // the software decode's errors end no stream: it stops by hand
	}
	s.twoLine = ClockedTwoLine(p.Proto)
	s.feature, s.longest, s.quiet = feature, longest, quiet
	s.pendA, s.pendB, s.bSeen = s.pendA[:0], s.pendB[:0], false
	s.next, s.base, s.haveSeq, s.retryAt = 0, 0, false, time.Time{}
	s.sampleS, s.ordAt, s.ordFrom = 0, 0, 0
	s.active, s.running, s.gap = true, false, false
	c.decodedTiming(plan.SampleS)
	if err := c.decodedFirst(); err != nil {
		c.decodedEnd(err)
		return err
	}
	c.cfg.Logf("decoded stream: %s: %s", d.Name, s.mode)
	return nil
}

// decodedFirstWait bounds the wait for a started stream's first block.
const decodedFirstWait = 2 * time.Second

// decodedFirst starts the board's stream and takes its first block: the
// rate the board delivers (StreamBlock.SampleS, which a plan need not
// state) is the start's verdict.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedFirst() error {
	s := &c.dec
	if err := c.decodedRun(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), decodedFirstWait)
	b, err := s.st.Lease(ctx)
	cancel()
	if err != nil {
		return nil // no block yet: the drain takes the first one and checks its rate
	}
	defer s.st.Release(b)
	if b.SampleS > 0 && math.Abs(b.SampleS-s.sampleS) > 1e-6*s.sampleS {
		if err := c.decodedRate(s.decoder.Name, s.p.Baud, b.SampleS, s.feature, s.twoLine); err != nil {
			return err
		}
	}
	c.decodedBlock(b)
	return nil
}

// decodedRate refuses a stream of a bus whose shortest feature (seconds)
// the board's stream at sampleS cannot resolve, or that streams more
// samples a second than the scope decodes.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedRate(bus string, baud int, sampleS, feature float64, two bool) error {
	if !(sampleS > 0) {
		return hal.Refuse("decoded stream", "the board's stream states no sample rate")
	}
	if feature < halDecodedMinSamples*sampleS {
		return hal.Refuse("decoded stream", fmt.Sprintf("%s at %d Bd needs a sample every %.3g us or less; the board streams one every %.3g us",
			bus, baud, feature/halDecodedMinSamples*1e6, sampleS*1e6))
	}
	lines := 1.0
	if two {
		lines = 2
	}
	if lines/sampleS > halDecodedBudget {
		return hal.Refuse("decoded stream", fmt.Sprintf("the board streams %.3g M samples a second, more than the scope decodes (%.3g M)",
			lines/sampleS/1e6, halDecodedBudget/1e6))
	}
	return nil
}

// decodedTiming sets the decoder for the stream's sample time: its window
// (quiet and longest in samples), its lead, how much it decodes at once.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedTiming(sampleS float64) {
	s := &c.dec
	if s.sampleS > 0 {
		s.ordAt, s.ordFrom = s.ord(s.next), s.next
	}
	s.sampleS = sampleS
	s.newDecoder()
	// a fifth of a second at a time, or twice Close, but at least once a
	// second (the SDS1000CML+'s gap-free push)
	s.pushN = max(1, min(int(math.Max(0.2, math.Min(1, 2*s.quiet))/sampleS), halDecodedPushMax))
	mode := fmt.Sprintf("gap-free, software decode of the %.3g us stream", sampleS*1e6)
	if s.byHand {
		mode += "; stops by hand (a software decode's errors end no stream)"
	}
	s.mu.Lock()
	s.mode = mode
	s.mu.Unlock()
}

// newDecoder starts the decoder afresh at the next stream sample, at the
// stream's sample time: its window (quiet and longest in samples) and its
// lead.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *halDecoded) newDecoder() {
	s.dec = decode.NewStreamDecoder(s.decodeFunc(), int(s.quiet/s.sampleS), int((s.longest+s.quiet)/s.sampleS))
	s.dec.SetSubframes(s.name == "spdif")
	s.dec.SetLead(int(decode.StreamLead(s.name, float64(s.p.Baud)) / s.sampleS))
	s.base = s.next
}

// decodeFunc is the registry decoder at the stream's sample time and the
// request's settings, with line B's learned level.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *halDecoded) decodeFunc() func(a, b []uint8) decode.Result {
	return func(a, b []uint8) decode.Result {
		p := s.p
		if !p.HaveThrB && s.bSeen && int(s.bHi)-int(s.bLo) >= 24 {
			p.ThresholdB, p.HaveThrB = (float64(s.bLo)+float64(s.bHi))/2, true
		}
		return s.decoder.Decode(a, b, s.sampleS, p)
	}
}

// decodedRun starts the board's stream of the committed state's channels
// at its fastest stream plan.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedRun() error {
	s := &c.dec
	c.stopArmed()
	r, plan, err := c.streamPlan()
	if err != nil {
		return err
	}
	if !slices.Contains(r.Channels, s.p.ChA) {
		return hal.Refuse("decoded stream", fmt.Sprintf("C%d went off", s.p.ChA+1))
	}
	if err := c.b.Acquirer().Configure(plan); err != nil {
		return err
	}
	if err := s.st.Start(hal.StreamSpec{Payload: hal.PayloadCodes, Channels: r.Channels, SampleS: plan.SampleS}); err != nil {
		return err
	}
	s.chans = r.Channels
	s.running, s.haveSeq = true, false
	if plan.SampleS != s.sampleS {
		c.decodedPush(true)
		c.decodedTiming(plan.SampleS)
	}
	return nil
}

// decodedHalt stops the board's stream for posted work; the stream starts
// again after a gap, and the captures' state is committed first.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedHalt() {
	s := &c.dec
	if err := s.st.Stop(); err != nil {
		c.cfg.Logf("decoded stream: stop: %v", err)
	}
	s.running, s.gap = false, true
	c.gen = 0
}

// decodedEnd ends the stream on the owner: the board's stream stops, the
// transcript ends (with err when it failed), and the captures' state is
// committed again.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedEnd(err error) {
	s := &c.dec
	if s.running {
		if e := s.st.Stop(); e != nil {
			c.cfg.Logf("decoded stream: stop: %v", e)
		}
	}
	s.running, s.active = false, false
	s.mu.Lock()
	s.done = true
	if err != nil && s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
	c.gen = 0
}

// decodedDrain moves the stream's blocks into the decoder for up to
// soundBlockWait and decodes once a push's worth is queued.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedDrain(ctx context.Context) {
	s := &c.dec
	end := time.Now().Add(soundBlockWait)
	for time.Now().Before(end) && s.active {
		lctx, cancel := context.WithDeadline(ctx, end)
		b, err := s.st.Lease(lctx)
		cancel()
		if err != nil {
			if ctx.Err() == nil && lctx.Err() == nil {
				c.decodedHalt() // the stream ended under us: started again, after a gap
				s.retryAt = time.Now().Add(100 * time.Millisecond)
			}
			break
		}
		c.decodedBlock(b)
		s.st.Release(b)
	}
	if len(s.pendA) >= s.pushN {
		c.decodedPush(false)
	}
}

// decodedBlock queues a block's lines on the 8-bit view; a block after a
// gap (a restart, a lost block) first ends the decode of what came before
// and marks the loss.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedBlock(b hal.StreamBlock) {
	s := &c.dec
	if b.SampleS > 0 && math.Abs(b.SampleS-s.sampleS) > 1e-6*s.sampleS {
		if err := c.decodedRate(s.decoder.Name, s.p.Baud, b.SampleS, s.feature, s.twoLine); err != nil {
			c.decodedEnd(err)
			return
		}
		c.decodedPush(true)
		c.decodedTiming(b.SampleS)
		s.gap = true
	}
	if jump := s.haveSeq && b.First > s.seqNext; s.gap || jump {
		c.decodedPush(true)
		if !s.active {
			return // the stop value came in the decode before the gap
		}
		if s.next > 0 {
			s.addEvent(decodedlines.Event{Kind: decodedlines.EventLoss, Sample: s.ord(s.next)})
			s.next++ // the gap's own sample: the decoder restarts past it
		}
		s.newDecoder()
		s.gap = false
	}
	ia, ib := slices.Index(s.chans, s.p.ChA), -1
	if s.twoLine {
		ib = slices.Index(s.chans, s.p.ChB)
	}
	n := 0
	for k, i := range []int{ia, ib} {
		if i < 0 || i >= len(b.Formats) {
			continue
		}
		fm := b.Formats[i]
		bytes, words := b.Run(i)
		dst := &s.pendA
		if k == 1 {
			dst = &s.pendB
		}
		at := len(*dst)
		for _, v := range bytes {
			*dst = append(*dst, fm.Code8(uint32(v)))
		}
		for _, v := range words {
			*dst = append(*dst, fm.Code8(uint32(v)))
		}
		if k == 0 {
			n = len(*dst) - at
		} else {
			s.learnB((*dst)[at:])
		}
	}
	s.next += uint64(n)
	s.seqNext, s.haveSeq = b.First+uint64(n), true
}

// learnB follows line B's extremes for its slicing level.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *halDecoded) learnB(b []uint8) {
	if len(b) == 0 {
		return
	}
	if !s.bSeen {
		s.bLo, s.bHi, s.bSeen = b[0], b[0], true
	}
	for _, v := range b {
		s.bLo, s.bHi = min(s.bLo, v), max(s.bHi, v)
	}
}

// decodedPush decodes the queued samples (flush: and closes the frames
// the decoder still holds, the stream ending or a gap coming).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c *HALCycle) decodedPush(flush bool) {
	s := &c.dec
	if s.dec == nil {
		return
	}
	a := s.pendA
	var b []uint8
	if len(s.pendB) == len(s.pendA) {
		b = s.pendB
	}
	s.pendA, s.pendB = s.pendA[:0], s.pendB[:0]
	for len(a) > 0 {
		n := min(len(a), halDecodedPushMax)
		var bn []uint8
		if b != nil {
			bn, b = b[:n], b[n:]
		}
		frames := s.dec.Push(a[:n], bn)
		a = a[n:]
		if s.emit(frames) {
			c.decodedEnd(nil)
			return
		}
	}
	if flush && s.emit(s.dec.Flush()) {
		c.decodedEnd(nil)
	}
}

// ord is stream sample i's 2 ns ordinal, the transcript's time base.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *halDecoded) ord(i uint64) uint64 {
	return s.ordAt + uint64(math.Round(float64(i-s.ordFrom)*s.sampleS/2e-9))
}

// emit queues frames as decoded events up to the frame holding the stop
// value; it reports that frame (the transcript ends there).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *halDecoded) emit(frames []decode.StreamFrame) bool {
	ord := func(i uint64) uint64 { return s.ord(s.base + i) }
	for _, f := range frames {
		s.addEvent(decodedlines.Event{Kind: decodedlines.EventStart, Sample: ord(f.I0), Value: decodedlines.StartNoteCode(streamFrameStart(f))})
		hit := false
		for _, sp := range f.Spans {
			switch sp.Kind {
			case SpanData:
				v := sp.Val & s.mask
				s.addEvent(decodedlines.Event{Kind: decodedlines.EventData, Sample: ord(sp.I0), Value: uint32(v), Valid: true}) //nolint:gosec // G115: v is masked to the unit width
				hit = hit || (s.trig == StreamOnValue && v == s.value)
			case SpanFrameError, SpanParityError, SpanGap:
				s.addEvent(decodedlines.Event{Kind: decodedlines.EventError, Sample: ord(sp.I0)})
			}
		}
		s.addEvent(decodedlines.Event{Kind: decodedlines.EventEnd, Sample: ord(f.I1)})
		if hit {
			// The frame's line starts at its named start or, unnamed, at
			// its first unit: the trigger mark goes on that line.
			first := ord(f.I0)
			if decodedlines.StartNoteCode(streamFrameStart(f)) == 0 {
				for _, sp := range f.Spans {
					if sp.Kind == SpanData || sp.Kind == SpanFrameError || sp.Kind == SpanParityError || sp.Kind == SpanGap {
						first = ord(sp.I0)
						break
					}
				}
			}
			s.addEvent(decodedlines.Event{Kind: decodedlines.EventTrigger, Sample: ord(f.I0)})
			s.mu.Lock()
			s.hit, s.first, s.last = true, first, ord(f.I1)
			s.mu.Unlock()
			return true
		}
	}
	return false
}

// streamFrameStart is a frame's start-of-frame text ("" none).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func streamFrameStart(f decode.StreamFrame) string {
	if len(f.Spans) > 0 && f.Spans[0].Kind == SpanSOF {
		return f.Spans[0].Text
	}
	return ""
}

// addEvent queues one event.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *halDecoded) addEvent(ev decodedlines.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.events)-s.evHead >= halDecodedEventsMax {
		if s.err == nil {
			s.err = errors.New("decoded host buffer overflow; transcript incomplete")
		}
		return
	}
	if s.evHead > 0 && s.evHead >= len(s.events)/2 {
		s.events = s.events[:copy(s.events, s.events[s.evHead:])]
		s.evHead = 0
	}
	ev.Epoch, ev.Sequence = s.epoch, s.seq
	s.seq++
	s.events = append(s.events, ev)
}
