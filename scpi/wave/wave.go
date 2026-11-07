// Package wave builds the waveform transfers of the SCPI dialects: the
// LeCroy WAVEDESC descriptor, IEEE 488.2 blocks, the first/sparsing/points
// window and the sample encodings. A dialect fills the descriptor's fields
// from its instrument; the layout is shared.
package wave

import (
	"encoding/binary"
	"math"
	"strconv"
)

// DescSize is the WAVEDESC length.
const DescSize = 346

// Desc is the WAVEDESC descriptor's fields, by their LeCroy names. Fields a
// dialect leaves zero are written as zero.
type Desc struct {
	Template          string // TEMPLATE_NAME
	CommType          int16  // 0 byte, 1 word samples
	CommOrder         int16  // byte order of words: 0 LSB first, 1 MSB first
	WaveArray1        int32  // bytes of sample data
	InstrumentName    string
	InstrumentNumber  int32
	Reserved112       int32
	WaveArrayCount    int32
	PointsPerScreen   int32
	FirstValid        int32
	LastValid         int32
	FirstPoint        int32
	Sparsing          int32
	SegmentIndex      int32
	SweepsPerAcq      int32 // offset 144
	PointsPerPair     int32 // offset 148
	VerticalGain      float32
	VerticalOffset    float32
	MaxValue          float32 // offset 164
	MinValue          float32 // offset 168
	NominalBits       int16
	NomSubarrayCount  int16
	HorizInterval     float32
	HorizOffset       float64
	PixelOffset       float64
	VertUnit          string
	HorUnit           string
	HorizUncertainty  float32
	RISSweeps         int16
	Timebase          int16
	VertCoupling      int16
	ProbeAtt          float32
	FixedVertGain     int16
	BandwidthLimit    int16
	VerticalVernier   float32
	AcqVerticalOffset float32
	WaveSource        int16
}

// Encode is d's 346 descriptor bytes, little-endian.
// TRLC-LINKS: REQ-DSO-024
func (d *Desc) Encode() []byte {
	b := make([]byte, DescSize)
	le := binary.LittleEndian
	i16 := func(off int, v int16) { le.PutUint16(b[off:], uint16(v)) } //nolint:gosec // G115: the field's two's-complement bits
	i32 := func(off int, v int32) { le.PutUint32(b[off:], uint32(v)) } //nolint:gosec // G115: the field's two's-complement bits
	f32 := func(off int, v float32) { le.PutUint32(b[off:], math.Float32bits(v)) }
	f64 := func(off int, v float64) { le.PutUint64(b[off:], math.Float64bits(v)) }
	str := func(off, n int, s string) { copy(b[off:off+n], s) }
	str(0, 16, "WAVEDESC")
	str(16, 16, d.Template)
	i16(32, d.CommType)
	i16(34, d.CommOrder)
	i32(36, DescSize)
	i32(60, d.WaveArray1)
	str(76, 16, d.InstrumentName)
	i32(92, d.InstrumentNumber)
	i32(112, d.Reserved112)
	i32(116, d.WaveArrayCount)
	i32(120, d.PointsPerScreen)
	i32(124, d.FirstValid)
	i32(128, d.LastValid)
	i32(132, d.FirstPoint)
	i32(136, d.Sparsing)
	i32(140, d.SegmentIndex)
	i32(144, d.SweepsPerAcq)
	i32(148, d.PointsPerPair)
	f32(156, d.VerticalGain)
	f32(160, d.VerticalOffset)
	f32(164, d.MaxValue)
	f32(168, d.MinValue)
	i16(172, d.NominalBits)
	i16(174, d.NomSubarrayCount)
	f32(176, d.HorizInterval)
	f64(180, d.HorizOffset)
	f64(188, d.PixelOffset)
	str(196, 48, d.VertUnit)
	str(244, 48, d.HorUnit)
	f32(292, d.HorizUncertainty)
	i16(322, d.RISSweeps)
	i16(324, d.Timebase)
	i16(326, d.VertCoupling)
	f32(328, d.ProbeAtt)
	i16(332, d.FixedVertGain)
	i16(334, d.BandwidthLimit)
	f32(336, d.VerticalVernier)
	f32(340, d.AcqVerticalOffset)
	i16(344, d.WaveSource)
	return b
}

// Block9 is payload as an IEEE 488.2 definite block with a nine-digit
// length: "#9<len>"+payload.
// TRLC-LINKS: REQ-DSO-024
func Block9(prefix string, payload []byte) []byte {
	n := strconv.Itoa(len(payload))
	out := make([]byte, 0, len(prefix)+11+len(payload))
	out = append(out, prefix...)
	out = append(out, '#', '9')
	for range 9 - len(n) {
		out = append(out, '0')
	}
	out = append(out, n...)
	return append(out, payload...)
}

// Block is payload as a definite block with the shortest length field:
// "#<digits><len>"+payload.
// TRLC-LINKS: REQ-DSO-024
func Block(prefix string, payload []byte) []byte {
	n := strconv.Itoa(len(payload))
	out := make([]byte, 0, len(prefix)+2+len(n)+len(payload))
	out = append(out, prefix...)
	out = append(out, '#', byte('0'+len(n))) //nolint:gosec // G115: the length's digit count, 1..9 for any record the scope holds
	out = append(out, n...)
	return append(out, payload...)
}

// Window selects the transferred points of a record: from First, every
// Sparse-th point, Points of them (0: all there are), at most Max points
// (0: no limit).
type Window struct {
	First, Sparse, Points, Max int
}

// Span is the window over a record of n points: the first index, the step
// and the count. ok is false when First is past the record.
// TRLC-LINKS: REQ-DSO-024
func (w Window) Span(n int) (first, step, count int, ok bool) {
	if w.First < 0 || w.First >= n {
		return 0, 0, 0, false
	}
	step = max(w.Sparse, 1)
	most := (n - w.First + step - 1) / step
	count = w.Points
	if count <= 0 || count > most {
		count = most
	}
	if w.Max > 0 && count*step > w.Max {
		count = min(count, w.Max/step)
	}
	return w.First, step, count, true
}

// Signed8 writes count codes of src from first, every step-th, as signed
// bytes about the 8-bit centre 128 (the code XOR 0x80).
// TRLC-LINKS: REQ-DSO-024
func Signed8(src []uint8, first, step, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = src[first+i*step] ^ 0x80
	}
	return out
}

// Signed16 writes count words of src from first, every step-th, as signed
// 16-bit values about the centre 0x8000, in the byte order given.
// TRLC-LINKS: REQ-DSO-024
func Signed16(src []uint16, first, step, count int, msbFirst bool) []byte {
	out := make([]byte, 2*count)
	for i := range count {
		v := src[first+i*step] ^ 0x8000
		if msbFirst {
			binary.BigEndian.PutUint16(out[2*i:], v)
		} else {
			binary.LittleEndian.PutUint16(out[2*i:], v)
		}
	}
	return out
}

// High8 writes count words of src from first, every step-th, as the signed
// byte of their high half about 0x80.
// TRLC-LINKS: REQ-DSO-024
func High8(src []uint16, first, step, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = uint8(src[first+i*step]>>8) ^ 0x80
	}
	return out
}
