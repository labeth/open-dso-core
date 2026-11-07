// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// Parts are the core engine's parts a scope's engine runs, each nil when it
// does not: the web routes of a part answer as a scope without it.
type Parts struct {
	Zones        Zones
	Serial       SerialTrigger
	Masks        MaskTest
	Bode         BodeCurve
	Sound        SoundStream
	SoundTakes   SoundTaker
	SoundLease   SoundArbiter
	SoundControl webserver.SoundControls
	ClientDecode ClientDecoder
	Leases       Leaser
	// RecordLease keeps the engine's records sampled every sampleS (no
	// envelope) for hold: a record download asks it while it waits. Nil:
	// records come from the frames as they are.
	RecordLease func(sampleS float64, hold time.Duration)
}

// Zones is the engine's zone trigger.
type Zones interface {
	SetZones(z []engine.Zone)
}

// SerialTrigger is the engine's protocol trigger.
type SerialTrigger interface {
	// SerialConfig is the configuration as armed; false when the engine
	// cannot read it back.
	SerialConfig() (engine.SerialParams, bool)
	SetSerialParams(p engine.SerialParams)
	// SerialMode is 0 off, 1 armed.
	SerialMode() int
}

// MaskTest is the engine's envelope mask test.
type MaskTest interface {
	SetMask(m *engine.Mask)
	// ClearMask removes the mask with its test and counts (and stops a
	// mask build under way on the display).
	ClearMask()
	// MaskGen names the mask installed: each install or clear counts one.
	MaskGen() uint64
	MaskFails() []engine.MaskFail
}

// MaskRunner is a mask test whose mode and counts the web drives and
// reads (engine.FrameMask): with it the shared settings take maskmode and
// maskclear, and the status carries the counts.
type MaskRunner interface {
	SetMaskMode(m int)
	ClearMaskFails()
	Counts() engine.MaskCounts
}

// BodeCurve is the engine's frequency-response curve.
type BodeCurve interface {
	BodePoints() []engine.BodePoint
}

// SoundStream is the engine's audio stream.
type SoundStream interface {
	LeaseSound(d time.Duration)
	SoundInfo() engine.SoundInfo
	SoundRead(from uint64, limit int, q1, q2 []uint16) engine.SoundBlock
	SoundWait(after uint64, timeout time.Duration) uint64
}

// SoundTaker is the engine's takes of the stream.
type SoundTaker interface {
	SoundTake() *engine.SoundTake
}

// SoundArbiter shares the stream between the display and the pages: false
// while the display holds the acquisition (the page is paused, with
// PausedText), else it renews the lease.
type SoundArbiter interface {
	SoundRemoteLease(who string, lease func()) bool
}

// PausedText is what a page shows while the display holds SOUND's
// acquisition.
const PausedText = "paused: the scope is in use"

// ClientDecoder is the engine's client decode: the digital stream's words
// for a page's worker and its decoded frames back.
type ClientDecoder interface {
	ReadClientWords(seq uint64, limit int, wait time.Duration, dst []engine.ClientChunk) (engine.ClientWords, func())
	PostClientFrames(body []byte) error
}

// Leaser is the engine's consumer leases: what a page's analyzer needs of
// the frames, kept alive by its polls. Each method is optional.
type Leaser interface {
	Lease(n webserver.Need) bool
}

// soundLease is how long a request keeps the stream running.
const soundLease = 3 * time.Second

// Triggers is the engine's zone and protocol triggers.
// TRLC-LINKS: REQ-DSO-162
func (s *Scope) Triggers() (webserver.Triggers, bool) {
	p := s.cfg.Parts
	return triggers{s}, p.Zones != nil || p.Serial != nil
}

// triggers is the engine's triggers as the web takes them.
type triggers struct{ s *Scope }

// SetZones installs the zone rectangles.
// TRLC-LINKS: REQ-DSO-162
func (t triggers) SetZones(zs []wire.Zone) {
	z := t.s.cfg.Parts.Zones
	if z == nil {
		return
	}
	out := make([]engine.Zone, 0, len(zs))
	for _, v := range zs {
		out = append(out, engine.Zone{DtLoS: v.DtLoS, DtHiS: v.DtHiS, CodeLo: v.CodeLo, CodeHi: v.CodeHi, Avoid: v.Avoid, Ch: v.Ch})
	}
	z.SetZones(out)
}

// SetZonesV installs zones given in volts at the probe tip: each becomes
// display codes at the channel's scale now. The zone qualifier compares
// display codes of C1 and C2, so a zone on another channel is refused.
// TRLC-LINKS: REQ-DSO-162
func (t triggers) SetZonesV(zs []wire.ZoneV) (int, error) {
	if t.s.cfg.Parts.Zones == nil {
		return 0, errors.New("no zone trigger")
	}
	off, vpc := t.s.scales().Scales()
	out := make([]wire.Zone, 0, len(zs))
	for _, z := range zs {
		if z.Ch < 0 || z.Ch > 1 {
			return 0, fmt.Errorf("channel C%d: the zone trigger takes C1 and C2", z.Ch+1)
		}
		lo, hi := zoneCode(z.VLo, off[z.Ch], vpc[z.Ch]), zoneCode(z.VHi, off[z.Ch], vpc[z.Ch])
		out = append(out, wire.Zone{DtLoS: z.DtLoS, DtHiS: z.DtHiS, CodeLo: min(lo, hi), CodeHi: max(lo, hi), Avoid: z.Avoid, Ch: z.Ch})
	}
	t.SetZones(out)
	return len(out), nil
}

// zoneCode is volts v at the tip as a display code about 128, clamped.
// TRLC-LINKS: REQ-DSO-162
func zoneCode(v, off, vpc float64) int {
	if !(vpc > 0) {
		return 128
	}
	return clampI(int(math.Round(128+(v+off)/vpc)), 0, 255)
}

// SerialConfig is the protocol trigger as armed (on the display, a phone
// or a page) and its mode: a page shows it rather than replace it with its
// own empty match.
// TRLC-LINKS: REQ-DSO-162
func (t triggers) SerialConfig() (any, int, bool) {
	st := t.s.cfg.Parts.Serial
	if st == nil {
		return nil, 0, false
	}
	cfg, ok := st.SerialConfig()
	if !ok {
		return nil, 0, false
	}
	return cfg, st.SerialMode(), true
}

// SetSerial installs a protocol-trigger configuration, bounded to what the
// receivers take. Arm and disarm are separate (/api/set serialmode). The
// SPI clock rate, the data delay and the receiver stay the scope's: its
// bus settings set them (the one path for the display, a phone and a
// page), so a page's post, one running an older page's scripts too, never
// replaces them.
// TRLC-LINKS: REQ-DSO-162
func (t triggers) SetSerial(decode func(v any) error) error {
	st := t.s.cfg.Parts.Serial
	var p engine.SerialParams
	if err := decode(&p); err != nil {
		return err
	}
	if st == nil {
		return errors.New("no protocol trigger")
	}
	BoundSerial(&p)
	if cur, ok := st.SerialConfig(); ok {
		p.SPIClockHz, p.SPIDelayNs, p.Fine = cur.SPIClockHz, cur.SPIDelayNs, cur.Fine
	}
	st.SetSerialParams(p)
	return nil
}

// BoundSerial clamps a page's protocol-trigger configuration to what the
// receivers take: a protocol id in range, two channels, an address in 7
// bits or any, a rate the fastest receiver takes, a word length and a
// match no longer than a record, each match value in its protocol's width.
// TRLC-LINKS: REQ-DSO-162
func BoundSerial(p *engine.SerialParams) {
	p.Proto = clampI(p.Proto, 0, engine.SerialProtocols-1) // 0 off … 10 flexray, 11 lin … 30 wiegand … 52 IR multi
	p.ChA, p.ChB = p.ChA&1, p.ChB&1
	p.RW = clampI(p.RW, 0, 2)
	if p.Addr > 127 || p.Addr < -1 {
		p.Addr = -1 // out of 7-bit range → "any" (never a silent never-match)
	}
	if p.Baud < 0 || p.Baud > 64_000_000 { // the fastest UART receiver: 62.5 MBd
		p.Baud = 0
	}
	if p.Bits != 0 {
		hi := 16
		switch p.Proto {
		case 21, 30: // SIRC's and Wiegand's frame length
			hi = 64
		case 34, 35, 44, 52: // an RC code's bits, Oregon's version, the PJON mode, the IR multi protocol
			hi = 64
		case 41: // a T55xx command's bits
			hi = 70
		}
		p.Bits = clampI(p.Bits, 1, hi)
	}
	if len(p.Bytes) > 64 { // a match pattern longer than a record is pointless
		p.Bytes = p.Bytes[:64]
	}
	maxValue := 255
	switch p.Proto {
	case 1, 4: // UART / Manchester configurable data word
		if p.Bits > 0 {
			maxValue = (1 << p.Bits) - 1
		}
	case 5: // SENT nibble
		maxValue = 15
	case 7: // MIL-STD-1553 word
		maxValue = 65535
	case 8: // ARINC 429 data field
		maxValue = 0x7ffff
	case 29, 30: // MDIO header and data, Wiegand fields
		maxValue = 0xffff
	case 26: // an S/PDIF sample
		maxValue = 0xffffff
	case 28: // an SWD data word's low 31 bits
		maxValue = 0x7fffffff
	case 31, 32: // an encoder's or a stepper's step direction
		maxValue = 1
	case 35: // an Oregon nibble
		maxValue = 15
	case 52: // an IR multi address or command
		maxValue = 0xffff
	case 49: // a cJTAG scan's low 31 bits
		maxValue = 0x7fffffff
	}
	for i := range p.Bytes {
		p.Bytes[i] = clampI(p.Bytes[i], 0, maxValue)
	}
}

// SerialProtocols lists the protocols the trigger takes.
// TRLC-LINKS: REQ-DSO-162
func (t triggers) SerialProtocols() any { return engine.SerialDecoderList() }

// TRLC-LINKS: REQ-DSO-162
func clampI(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Masks is the engine's mask test.
// TRLC-LINKS: REQ-DSO-162
func (s *Scope) Masks() (webserver.Masks, bool) {
	return masks{s}, s.cfg.Parts.Masks != nil
}

// masks is the engine's mask test as the web takes it.
type masks struct{ s *Scope }

// SetMask installs a mask built on the page (display-window columns, win =
// the window's columns at build time); with the frame's timebase and
// sample interval it compares only with frames of that identity.
// TRLC-LINKS: REQ-DSO-162
func (m masks) SetMask(mk wire.Mask) {
	m.s.cfg.Parts.Masks.SetMask(&engine.Mask{
		Lo: mk.Lo, Hi: mk.Hi, WinCols: mk.WinCols, Ch: mk.Ch,
		FrameIdent: mk.TdivS > 0 && mk.SampleS > 0, TdivS: mk.TdivS, SampleS: mk.SampleS, PeakDetect: mk.PeakDetect,
	})
}

// ClearMask removes the mask with its test and counts.
// TRLC-LINKS: REQ-DSO-162
func (m masks) ClearMask() { m.s.cfg.Parts.Masks.ClearMask() }

// MaskGen names the mask installed.
// TRLC-LINKS: REQ-DSO-162
func (m masks) MaskGen() uint64 { return m.s.cfg.Parts.Masks.MaskGen() }

// MaskFails is the ring of captured failing frames, most recent last.
// TRLC-LINKS: REQ-DSO-162
func (m masks) MaskFails() []wire.MaskFail {
	ring := m.s.cfg.Parts.Masks.MaskFails()
	out := make([]wire.MaskFail, len(ring))
	for i, f := range ring {
		out[i] = wire.MaskFail{
			C1: f.C1, C2: f.C2, Valid: f.Valid, Seq: f.Seq, EdgeX: f.EdgeX, SampleS: f.SampleS, WinCols: f.WinCols,
			AtNs: f.AtNs, FailCol: f.FailCol, FailCode: f.FailCode, FailSample: f.FailSample,
		}
	}
	return out
}

// VertScales are the channels' offset volts and volts a code now.
// TRLC-LINKS: REQ-DSO-164
func (m masks) VertScales() (off, vpc [2]float64) { return m.s.scales().Scales() }

// Bode is the engine's frequency-response curve.
// TRLC-LINKS: REQ-DSO-162
func (s *Scope) Bode() (webserver.Bode, bool) {
	return bode{s.cfg.Parts.Bode}, s.cfg.Parts.Bode != nil
}

// bode is the engine's curve as wire points.
type bode struct{ b BodeCurve }

// TRLC-LINKS: REQ-DSO-162
func (b bode) BodePoints() []wire.BodePoint {
	pts := b.b.BodePoints()
	out := make([]wire.BodePoint, len(pts))
	for i, p := range pts {
		out[i] = wire.BodePoint{FreqHz: p.FreqHz, GainDB: p.GainDB, PhaseDeg: p.PhaseDeg}
	}
	return out
}

// Sound is the engine's audio stream, held through the arbitration between
// the display and the pages where the scope has one.
// TRLC-LINKS: REQ-DSO-265
func (s *Scope) Sound() (webserver.Sound, bool) {
	src := s.cfg.Parts.Sound
	if src == nil {
		return nil, false
	}
	snd := soundStream{s: s, src: src}
	if tk := s.cfg.Parts.SoundTakes; tk != nil {
		return soundWithTakes{snd, tk}, true
	}
	return snd, true
}

// SoundControls is SOUND's settings, actions and readouts.
// TRLC-LINKS: REQ-DSO-274
func (s *Scope) SoundControls() (webserver.SoundControls, bool) {
	c := s.cfg.Parts.SoundControl
	return c, c != nil
}

// soundStream is the engine's stream as the pages read it.
type soundStream struct {
	s   *Scope
	src SoundStream
}

// Lease holds the stream for a page's request.
// TRLC-LINKS: REQ-DSO-277, REQ-DSO-265
func (sn soundStream) Lease(who string) string {
	if a := sn.s.cfg.Parts.SoundLease; a != nil {
		if !a.SoundRemoteLease(who, func() { sn.src.LeaseSound(soundLease) }) {
			return PausedText
		}
		return ""
	}
	sn.src.LeaseSound(soundLease)
	return ""
}

// Info is the stream's state.
// TRLC-LINKS: REQ-DSO-265
func (sn soundStream) Info() wire.SoundInfo {
	in := sn.src.SoundInfo()
	return wire.SoundInfo{
		State: in.State, Active: in.Active, RateHz: in.RateHz, Next: in.Next, Losses: in.Losses,
		Marks: in.Marks, MarkCh: in.MarkCfg.Ch, MarkLevel: in.MarkCfg.Level, Err: in.Err,
	}
}

// Read copies the stream from ordinal from, its scales as volts at the tip.
// TRLC-LINKS: REQ-DSO-265
func (sn soundStream) Read(from uint64, limit int) wire.SoundBlock {
	return sn.s.soundBlock(sn.src.SoundRead(from, limit, nil, nil))
}

// Wait parks until sample after has come, or timeout.
// TRLC-LINKS: REQ-DSO-265
func (sn soundStream) Wait(after uint64, timeout time.Duration) { sn.src.SoundWait(after, timeout) }

// soundWithTakes is the stream with the engine's takes.
type soundWithTakes struct {
	soundStream
	tk SoundTaker
}

// Take is the take shown; nil with none.
// TRLC-LINKS: REQ-DSO-275
func (st soundWithTakes) Take() *wire.SoundTake {
	t := st.tk.SoundTake()
	if t == nil {
		return nil
	}
	return &wire.SoundTake{
		SoundBlock: st.s.soundBlock(t.SoundBlock), ID: t.ID, Why: t.Why,
		HasTrig: t.HasTrig, Trig: t.Trig, TrigCh: t.TrigCh, TrigRising: t.TrigRising, TrigLevel: t.TrigLevel,
	}
}

// soundBlock is an engine block with its scales as volts at the tip.
// TRLC-LINKS: REQ-DSO-265
func (s *Scope) soundBlock(b engine.SoundBlock) wire.SoundBlock {
	return wire.SoundBlock{
		First: b.First, N: b.N, Q1: b.Q1, Q2: b.Q2, RateHz: b.RateHz,
		Scales: s.soundScales(b.Scales), Gaps: b.Gaps, Marks: b.Marks, Next: b.Next,
	}
}

// soundScales are the stream's scales as volts at the probe tip.
// TRLC-LINKS: REQ-DSO-265
func (s *Scope) soundScales(scales []engine.SoundScale) []wire.SoundScale {
	var out []wire.SoundScale
	sc := s.scales()
	for _, v := range scales {
		var o wire.SoundScale
		o.From = v.From
		for ch := range 2 {
			vpc, off := v.SoundVolts(ch)
			p := sc.Probe(ch)
			if !(p > 0) {
				p = 1
			}
			o.VPC[ch], o.Off[ch] = vpc*p, off*p
		}
		out = append(out, o)
	}
	return out
}

// ClientDecode is the engine's client decode.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (s *Scope) ClientDecode() (webserver.ClientDecode, bool) {
	cd := s.cfg.Parts.ClientDecode
	return clientDecode{cd}, cd != nil
}

// clientDecode hands the engine's chunks to the server as wire chunks.
type clientDecode struct{ cd ClientDecoder }

// engineChunks are reads' engine chunk lists, reused: a page at the densest
// stream asks a few times a second.
var engineChunks = sync.Pool{New: func() any { return new([]engine.ClientChunk) }}

// ReadWords reads the ring's chunks from seq; their words stay the
// engine's ring's (pinned until release), written straight to the socket.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c clientDecode) ReadWords(seq uint64, limit int, wait time.Duration, dst []wire.WordChunk) (wire.Words, func()) {
	ec, ok := engineChunks.Get().(*[]engine.ClientChunk)
	if !ok {
		ec = new([]engine.ClientChunk)
	}
	cw, release := c.cd.ReadClientWords(seq, limit, wait, (*ec)[:0])
	*ec = cw.Chunks
	for _, ch := range cw.Chunks {
		dst = append(dst, wire.WordChunk{Index: ch.Index, Words: ch.Words, Break: ch.Break, Check: ch.Check, Enc: ch.Enc})
	}
	return wire.Words{Info: cw.Info, Chunks: dst, Dropped: cw.Dropped, Next: cw.Next, Lag: cw.Lag}, func() {
		release()
		clear(*ec)
		engineChunks.Put(ec)
	}
}

// ReplyMax bounds a read (the engine's tap).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c clientDecode) ReplyMax() int { return engine.ClientReplyMax }

// PostFrames queues a page's frames; a body that is not frames is the
// request's fault.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (c clientDecode) PostFrames(body []byte) error {
	err := c.cd.PostClientFrames(body)
	if errors.Is(err, engine.ErrClientFramesBody) {
		return webserver.BadRequest{Err: err}
	}
	return err
}

// stream is the UI model's decoded stream: its transcript as data, in the
// compact binary line format and as the display's list writes it. A part
// the UI model lacks answers as no stream.
type stream struct{ ui webserver.UIModel }

// StreamLines is the transcript from an absolute line index.
// TRLC-LINKS: REQ-DSO-018
func (st stream) StreamLines(from, limit int) (streamwire.LinesPage, bool) {
	src, ok := st.ui.(interface {
		StreamLines(from, limit int) (streamwire.LinesPage, bool)
	})
	if !ok {
		return streamwire.LinesPage{}, false
	}
	return src.StreamLines(from, limit)
}

// StreamLinesBinary is the transcript from a line in the binary format.
// TRLC-LINKS: REQ-DSO-018
func (st stream) StreamLinesBinary(from int, dst []byte) (int, bool) {
	src, ok := st.ui.(interface {
		StreamLinesBinary(from int, dst []byte) (int, bool)
	})
	if !ok {
		return 0, false
	}
	return src.StreamLinesBinary(from, dst)
}

// StreamWindow is the newest rows as the display's list writes them.
// TRLC-LINKS: REQ-DSO-018
func (st stream) StreamWindow(rows int) (streamwire.Page, bool) {
	src, ok := st.ui.(interface {
		StreamWindow(rows int) (streamwire.Page, bool)
	})
	if !ok {
		return streamwire.Page{}, false
	}
	return src.StreamWindow(rows)
}
