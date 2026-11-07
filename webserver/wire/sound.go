// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package wire

import (
	"encoding/binary"
	"encoding/json"
	"reflect"
)

// SoundBinMagic starts every /api/sound.bin reply ("SB", version 1).
var SoundBinMagic = [3]byte{'S', 'B', 1}

// SoundMark is a level crossing the FPGA marked: the sample (stream
// ordinal) whose value crossed, the channel and direction.
type SoundMark struct {
	At     uint64 `json:"at"`
	Ch     int    `json:"ch"`
	Rising bool   `json:"rising"`
	N      int    `json:"n,omitempty"` // crossings since the last read (the image keeps the newest)
}

// SoundScale is a scale as volts at the probe tip: v = (q/256 - 128)*vpc - off.
// Chans names the scope channel (0-based) each of the stream's two lanes
// carries from From on; a scope of two channels leaves it out (C1 and C2).
type SoundScale struct {
	From  uint64     `json:"from"`
	VPC   [2]float64 `json:"vpc"`
	Off   [2]float64 `json:"off"`
	Chans []int      `json:"chans,omitempty"`
}

// SoundBinHeader is the JSON header of a /api/sound.bin reply.
type SoundBinHeader struct {
	First    uint64       `json:"first"`
	N        int          `json:"n"`
	Next     uint64       `json:"next"`
	Rate     float64      `json:"rate"`
	Scales   []SoundScale `json:"scales"`
	Gaps     []uint64     `json:"gaps"`
	Marks    []SoundMark  `json:"marks"`
	State    string       `json:"state"`
	Active   bool         `json:"active"`
	Losses   int          `json:"losses"`
	MarksOn  bool         `json:"marks_on"`
	MarkCh   int          `json:"mark_ch"`
	MarkV    float64      `json:"mark_v"` // the marks' level, volts at the tip
	ServerMS int64        `json:"server_ms"`
	Err      string       `json:"err,omitempty"`
	Paused   bool         `json:"paused,omitempty"` // the display holds the acquisition: no samples until the page takes the scope
}

// SoundTakeHeader is the JSON header of a /api/sound/take.bin reply.
type SoundTakeHeader struct {
	ID         uint64       `json:"id"`
	Why        string       `json:"why"`
	First      uint64       `json:"first"`
	N          int          `json:"n"`
	Rate       float64      `json:"rate"`
	Scales     []SoundScale `json:"scales"`
	Gaps       []uint64     `json:"gaps"`
	Marks      []SoundMark  `json:"marks"`
	HasTrig    bool         `json:"has_trig"`
	Trig       uint64       `json:"trig"`
	TrigCh     int          `json:"trig_ch"`
	TrigRising bool         `json:"trig_rising"`
	TrigV      float64      `json:"trig_v"` // the trigger's level, volts at the tip
}

// LevelVolts is an 8-bit code on channel ch as volts at the tip, at the
// newest scale (0 with none).
// TRLC-LINKS: REQ-SDS-272, REQ-SDS-275
func LevelVolts(sc []SoundScale, ch int, code uint8) float64 {
	k := len(sc)
	if k == 0 {
		return 0
	}
	ch &= 1
	return (float64(code)-128)*sc[k-1].VPC[ch] - sc[k-1].Off[ch]
}

// EncodeSoundBin lays out a header and the samples in /api/sound.bin's
// format: magic, the JSON's length, the JSON, then the little-endian Q8.8
// codes of C1 and of C2.
// TRLC-LINKS: REQ-SDS-265, REQ-SDS-275
func EncodeSoundBin(h any, q1, q2 []uint16, n int) []byte {
	hj, err := json.Marshal(h)
	if err != nil {
		// A scale or level that is no number (a front end not set yet) goes
		// as null: an empty header broke the page's whole stream.
		hj, _ = json.Marshal(JSONSafe(reflect.ValueOf(h)))
	}
	buf := make([]byte, 8, 8+len(hj)+4*n)
	copy(buf, SoundBinMagic[:])
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(hj))) //nolint:gosec // G115: a JSON header of a few KB
	buf = append(buf, hj...)
	for _, seg := range [][]uint16{q1, q2} {
		buf = append(buf, LEBytes(seg)...) // the words' little-endian bytes
	}
	return buf
}
