// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/webserver/wire"
)

// SoundLog2 is the audio stream's decimation (sound.StreamLog2).
const SoundLog2 = 13

// SoundRingLog2 sizes the ring: 2^20 samples a channel, 17 s.
const SoundRingLog2 = 20

// SoundLeaseMax bounds one lease.
const SoundLeaseMax = 10 * time.Second

// SoundScale is the front end's scale over a stretch of the stream: volts at
// the analog input are (code-128)*VdivV/25 - OffV, and at the probe tip
// times the probe factor.
type SoundScale struct {
	From  uint64     `json:"from"` // the first sample it applies to
	VdivV [2]float64 `json:"vdiv_v"`
	OffV  [2]float64 `json:"off_v"`
	// Chans names the scope channel each lane carries (lane i is channel
	// Chans[i]); nil: C1 and C2, as a two-channel scope streams.
	Chans []int `json:"chans,omitempty"`
}

// LaneOf is the lane carrying scope channel ch at scale s, -1 when none
// does.
// TRLC-LINKS: REQ-DSO-265
func (s SoundScale) LaneOf(ch int) int {
	if s.Chans == nil {
		if ch == 0 || ch == 1 {
			return ch
		}
		return -1
	}
	for i, c := range s.Chans {
		if c == ch && i < 2 {
			return i
		}
	}
	return -1
}

// Lane is block b's samples of scope channel ch (nil when no lane carries
// it), by the lanes of its newest scale.
// TRLC-LINKS: REQ-DSO-265
func (b SoundBlock) Lane(ch int) []uint16 {
	var sc SoundScale
	if k := len(b.Scales); k > 0 {
		sc = b.Scales[k-1]
	}
	switch sc.LaneOf(ch) {
	case 0:
		return b.Q1
	case 1:
		return b.Q2
	}
	return nil
}

// SoundMark is a level crossing the FPGA marked (wire.SoundMark: it goes to
// the pages as it is).
type SoundMark = wire.SoundMark

// SoundMarkConfig asks the FPGA for marks: the channel, the level (8-bit
// code), hysteresis codes, and which directions.
type SoundMarkConfig struct {
	Ch      int   `json:"ch"`
	Level   uint8 `json:"level"`
	Hyst    uint8 `json:"hyst"`
	Rising  bool  `json:"rising"`
	Falling bool  `json:"falling"`
}

// SoundInfo is the audio stream's state.
type SoundInfo struct {
	State    string          `json:"state"` // "off", "starting", "streaming", or why it cannot run
	Active   bool            `json:"active"`
	RateHz   float64         `json:"rate_hz"`
	Next     uint64          `json:"next"`   // the next sample's ordinal
	Oldest   uint64          `json:"oldest"` // the oldest sample still held
	Losses   int             `json:"losses"`
	Marks    bool            `json:"marks"` // the image marks crossings
	MarkCfg  SoundMarkConfig `json:"mark_cfg"`
	Started  time.Time       `json:"started"`
	LastData time.Time       `json:"last_data"`
	Err      string          `json:"err,omitempty"`
	// Takes (sound_take.go): "live", "armed", "triggered" or "stopped" (or
	// why a trigger cannot run), the take shown (0 none) and the times.
	Mode     string  `json:"mode"`
	Take     uint64  `json:"take"`
	PreS     float64 `json:"pre_s"`
	PostS    float64 `json:"post_s"`
	TrigGate bool    `json:"trig_gate"`
}

// SoundBlock is a copy of the ring: samples First..First+N-1 of each
// channel (Q8.8 codes), with the scales, gaps and marks inside it.
type SoundBlock struct {
	First  uint64
	N      int
	Q1, Q2 []uint16
	RateHz float64
	Scales []SoundScale // the first applies from First (or before)
	Gaps   []uint64     // samples a loss came before
	Marks  []SoundMark
	Next   uint64 // the ring's next ordinal when copied
}

// SoundRing holds the stream's newest samples, shared between the
// acquisition owner (writer) and the consumers.
type SoundRing struct {
	Mu     sync.Mutex
	Q      [2][]uint16
	Next   uint64
	scales []SoundScale
	Gaps   []uint64
	Marks  []SoundMark
	wake   chan struct{} // closed and replaced on every append
	Last   time.Time
	size   int    // samples a channel when allocated
	from0  uint64 // nothing before this ordinal is held (a release)
}

// NewSoundRing makes an empty ring of 2^log2 samples a channel.
// TRLC-LINKS: REQ-DSO-265
func NewSoundRing(log2 int) *SoundRing {
	n := 1 << log2
	return &SoundRing{Q: [2][]uint16{make([]uint16, n), make([]uint16, n)}, wake: make(chan struct{}), size: n}
}

// Release lets the samples go (4 MB) while no stream runs; the next push
// allocates them again. Ordinals go on from where they were.
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) Release() {
	r.Mu.Lock()
	r.Q = [2][]uint16{}
	r.from0 = r.Next
	r.scales, r.Gaps, r.Marks = nil, nil, nil
	r.Mu.Unlock()
}

// OldestLocked is the oldest ordinal held (r.Mu held).
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) OldestLocked() uint64 {
	old := uint64(0)
	if size := uint64(len(r.Q[0])); r.Next > size {
		old = r.Next - size
	}
	return max(old, r.from0)
}

// PushWords appends stream words (CH1 Q8.8, CH2 Q8.8, little-endian) at
// scale sc; gap says words were lost before them.
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) PushWords(raw []byte, sc SoundScale, gap bool, now time.Time) {
	n := len(raw) / 4
	if n == 0 && !gap {
		return
	}
	r.Mu.Lock()
	if len(r.Q[0]) == 0 {
		r.Q = [2][]uint16{make([]uint16, r.size), make([]uint16, r.size)}
	}
	mask := uint64(len(r.Q[0]) - 1) //nolint:gosec // G115: the ring is a non-empty power of two
	if gap && r.Next > 0 && (len(r.Gaps) == 0 || r.Gaps[len(r.Gaps)-1] != r.Next) {
		r.Gaps = append(r.Gaps, r.Next)
	}
	if k := len(r.scales); k == 0 || r.scales[k-1].VdivV != sc.VdivV || r.scales[k-1].OffV != sc.OffV || !slices.Equal(r.scales[k-1].Chans, sc.Chans) {
		sc.From = r.Next
		r.scales = append(r.scales, sc)
	}
	q1, q2 := r.Q[0], r.Q[1]
	for i := range n {
		j := (r.Next + uint64(i)) & mask
		q1[j] = uint16(raw[4*i]) | uint16(raw[4*i+1])<<8
		q2[j] = uint16(raw[4*i+2]) | uint16(raw[4*i+3])<<8
	}
	r.Next += uint64(n)
	r.trimLocked()
	if n > 0 {
		r.Last = now
	}
	close(r.wake)
	r.wake = make(chan struct{})
	r.Mu.Unlock()
}

// PushQ appends samples q1, q2 (each lane's Q8.8 codes, as many as each)
// at scale sc; gap says samples were lost before them.
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) PushQ(q1, q2 []uint16, sc SoundScale, gap bool, now time.Time) {
	n := min(len(q1), len(q2))
	if n == 0 && !gap {
		return
	}
	r.Mu.Lock()
	if len(r.Q[0]) == 0 {
		r.Q = [2][]uint16{make([]uint16, r.size), make([]uint16, r.size)}
	}
	mask := uint64(len(r.Q[0]) - 1) //nolint:gosec // G115: the ring is a non-empty power of two
	if gap && r.Next > 0 && (len(r.Gaps) == 0 || r.Gaps[len(r.Gaps)-1] != r.Next) {
		r.Gaps = append(r.Gaps, r.Next)
	}
	if k := len(r.scales); k == 0 || r.scales[k-1].VdivV != sc.VdivV || r.scales[k-1].OffV != sc.OffV || !slices.Equal(r.scales[k-1].Chans, sc.Chans) {
		sc.From = r.Next
		r.scales = append(r.scales, sc)
	}
	for i := range n {
		j := (r.Next + uint64(i)) & mask
		r.Q[0][j], r.Q[1][j] = q1[i], q2[i]
	}
	r.Next += uint64(n)
	r.trimLocked()
	if n > 0 {
		r.Last = now
	}
	close(r.wake)
	r.wake = make(chan struct{})
	r.Mu.Unlock()
}

// AddMarks records marks (ordinals already in the stream's sample count).
// TRLC-LINKS: REQ-DSO-272
func (r *SoundRing) AddMarks(ms []SoundMark) {
	if len(ms) == 0 {
		return
	}
	r.Mu.Lock()
	r.Marks = append(r.Marks, ms...)
	r.trimLocked()
	r.Mu.Unlock()
}

// trimLocked drops scales, gaps and marks older than the ring (keeping the
// scale in force at its oldest sample).
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) trimLocked() {
	old := r.OldestLocked()
	for len(r.scales) > 1 && r.scales[1].From <= old {
		r.scales = r.scales[1:]
	}
	for len(r.Gaps) > 0 && r.Gaps[0] < old {
		r.Gaps = r.Gaps[1:]
	}
	for len(r.Marks) > 0 && r.Marks[0].At < old {
		r.Marks = r.Marks[1:]
	}
	if len(r.Marks) > 4096 {
		r.Marks = r.Marks[len(r.Marks)-4096:]
	}
}

// Read copies up to limit samples from ordinal from (moved up to the oldest
// held) into q1, q2 (grown as needed).
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) Read(from uint64, limit int, q1, q2 []uint16, rate float64) SoundBlock {
	r.Mu.Lock()
	defer r.Mu.Unlock()
	b := SoundBlock{RateHz: rate, Next: r.Next}
	if old := r.OldestLocked(); from < old {
		from = old
	}
	if from > r.Next {
		from = r.Next
	}
	n := int(min(uint64(limit), r.Next-from)) //nolint:gosec // G115: the result is at most the samples held
	b.First, b.N = from, n
	b.Q1, b.Q2 = grow16(q1, n), grow16(q2, n)
	if n > 0 { // at most two stretches: to the ring's end, then from its start
		at := int(from & uint64(len(r.Q[0])-1)) //nolint:gosec // G115: the ring is a non-empty power of two
		k := copy(b.Q1, r.Q[0][at:])
		copy(b.Q1[k:], r.Q[0])
		copy(b.Q2, r.Q[1][at:])
		copy(b.Q2[k:], r.Q[1])
	}
	end := from + uint64(n)
	for i, s := range r.scales {
		if s.From < end && (i+1 == len(r.scales) || r.scales[i+1].From > from) {
			b.Scales = append(b.Scales, s)
		}
	}
	for _, g := range r.Gaps {
		if g > from && g < end {
			b.Gaps = append(b.Gaps, g)
		}
	}
	for _, m := range r.Marks {
		if m.At >= from && m.At < end {
			b.Marks = append(b.Marks, m)
		}
	}
	return b
}

// grow16 is q with n entries, reallocated only when too small.
// TRLC-LINKS: REQ-DSO-265
func grow16(q []uint16, n int) []uint16 {
	if cap(q) < n {
		return make([]uint16, n)
	}
	return q[:n]
}

// Latest copies the newest n samples.
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) Latest(n int, q1, q2 []uint16, rate float64) SoundBlock {
	r.Mu.Lock()
	next := r.Next
	r.Mu.Unlock()
	from := uint64(0)
	if next > uint64(n) { //nolint:gosec // G115: n is a non-negative sample count
		from = next - uint64(n) //nolint:gosec // G115: n is a non-negative sample count
	}
	return r.Read(from, n, q1, q2, rate)
}

// Wait returns once the ring holds samples past after, or at timeout.
// TRLC-LINKS: REQ-DSO-265
func (r *SoundRing) Wait(after uint64, timeout time.Duration) uint64 {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		r.Mu.Lock()
		next, wake := r.Next, r.wake
		r.Mu.Unlock()
		if next > after {
			return next
		}
		select {
		case <-wake:
		case <-deadline.C:
			return next
		}
	}
}

// UnwrapWord extends the image's 32-bit word count w to the stream's
// ordinal near ref (marks can run ahead of the words drained).
// TRLC-LINKS: REQ-DSO-272
func UnwrapWord(w uint32, ref uint64) uint64 {
	v := ref&^0xffffffff | uint64(w)
	switch {
	case v > ref+1<<31:
		if v >= 1<<32 {
			v -= 1 << 32
		}
	case v+1<<31 < ref:
		v += 1 << 32
	}
	return v
}

// ChanVolts is SoundVolts for the lane carrying scope channel ch (0, 0
// when none does).
// TRLC-LINKS: REQ-DSO-265
func (s SoundScale) ChanVolts(ch int) (vpc, off float64) {
	lane := s.LaneOf(ch)
	if lane < 0 {
		return 0, 0
	}
	return s.SoundVolts(lane)
}

// SoundVolts is the scale's volts per code and offset at the analog input
// for lane ch (times the probe factor at the tip).
// TRLC-LINKS: REQ-DSO-265
func (s SoundScale) SoundVolts(ch int) (vpc, off float64) {
	ch &= 1
	if !(s.VdivV[ch] > 0) || math.IsInf(s.VdivV[ch], 0) {
		return 0, 0
	}
	return s.VdivV[ch] / 25, s.OffV[ch]
}
