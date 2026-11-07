// Package sample holds the frame every part of the product passes around: the
// engine publishes it, the renderer draws it, the web server and SCPI serve
// it. It is a leaf package (the HAL refers to it, not the other way round) so
// that a board's readout fills the same type the consumers read.
//
// A frame is not generic over its sample type. One trace may carry 8-bit
// codes and finer precision words at once (the SDS1000CML+ publishes raw
// codes beside Q8.8 conditioned words), and a type parameter would have to
// thread through the engine, the UI model, the renderer and the web server.
// Instead a trace holds Codes8 or Codes16 with a CodeFormat that says how
// the stored words map to the converter; kernels pick their implementation
// per frame.
package sample

import (
	"errors"
	"fmt"
	"math"
	"strings"
)

// Content is what a frame's samples are, independent of how they are stored.
type Content uint8

// The content kinds. The zero value is unknown: a frame that does not say
// what it holds must not be fed to a consumer that needs a particular kind.
const (
	RawFullRate  Content = iota + 1 // every converter sample at the acquisition rate
	RawDecimated                    // every Nth sample, or a filtered decimation
	// PeakPairs: the two extremes of each bucket, interleaved in the
	// codes. Without Ordered each pair is (min, max); with Ordered the two
	// are in the order they occurred, so the codes read as samples. Either
	// way SampleS and TriggerAt count codes (half a bucket each), not
	// pairs.
	PeakPairs
	Resampled     // interpolated or resampled to N points
	DisplayDomain // already in a display code space
)

var contentNames = [...]string{"", "raw-full-rate", "raw-decimated", "peak-pairs", "resampled", "display-domain"}

// String returns the kind's profile name, or "unknown".
// TRLC-LINKS: REQ-DSO-1003
func (c Content) String() string {
	if int(c) < len(contentNames) && c != 0 {
		return contentNames[c]
	}
	return "unknown"
}

// MarshalText returns the kind's profile name.
// TRLC-LINKS: REQ-DSO-1003
func (c Content) MarshalText() ([]byte, error) {
	if c == 0 || int(c) >= len(contentNames) {
		return nil, fmt.Errorf("sample: content %d has no name", c)
	}
	return []byte(contentNames[c]), nil
}

// UnmarshalText parses a profile name such as "peak-pairs".
// TRLC-LINKS: REQ-DSO-1003
func (c *Content) UnmarshalText(b []byte) error {
	for i, n := range contentNames {
		if i > 0 && n == string(b) {
			*c = Content(i)
			return nil
		}
	}
	return fmt.Errorf("sample: unknown content %q", b)
}

// ContentSet is a set of content kinds, as a profile or a consumer states it.
type ContentSet uint8

// ContentSetOf returns the set holding cs.
// TRLC-LINKS: REQ-DSO-1003
func ContentSetOf(cs ...Content) ContentSet {
	var s ContentSet
	for _, c := range cs {
		s |= 1 << c
	}
	return s
}

// Has reports whether c is in s.
// TRLC-LINKS: REQ-DSO-1003
func (s ContentSet) Has(c Content) bool { return c != 0 && s&(1<<c) != 0 }

// String lists the set's names, comma separated.
// TRLC-LINKS: REQ-DSO-1003
func (s ContentSet) String() string {
	var names []string
	for c := RawFullRate; c <= DisplayDomain; c++ {
		if s.Has(c) {
			names = append(names, c.String())
		}
	}
	return strings.Join(names, ",")
}

// MarshalText lists the set's names, comma separated.
// TRLC-LINKS: REQ-DSO-1003
func (s ContentSet) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText parses comma-separated names; empty text is the empty set.
// TRLC-LINKS: REQ-DSO-1003
func (s *ContentSet) UnmarshalText(b []byte) error {
	var out ContentSet
	for f := range strings.SplitSeq(string(b), ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		var c Content
		if err := c.UnmarshalText([]byte(f)); err != nil {
			return err
		}
		out |= 1 << c
	}
	*s = out
	return nil
}

// ContentFlag qualifies a frame's samples.
type ContentFlag uint16

// The flags.
const (
	// Ordered: each peak pair is in time order, decodable as samples at
	// SampleS. A board sets it only when it knows which extreme came first;
	// a pair whose order the hardware does not record is (min, max) and
	// unflagged.
	Ordered   ContentFlag = 1 << iota
	Interp                // the consumer should interpolate between samples
	Roll                  // a scrolling roll view, newest sample last, untriggered
	Held                  // a held frame shown while stopped
	Segmented             // one of several segments of a record
	Precision             // traces carry Precision words beside their codes
	// Envelope: the traces' Min*/Max* hold a display envelope, one (min,
	// max) per screen column, to draw instead of the codes (an untriggered
	// band over a record faster than the screen).
	Envelope
)

// Has reports whether every flag in x is set.
// TRLC-LINKS: REQ-DSO-1003
func (f ContentFlag) Has(x ContentFlag) bool { return f&x == x }

// Set sets the flags in x when on, else clears them.
// TRLC-LINKS: REQ-DSO-1003
func (f *ContentFlag) Set(x ContentFlag, on bool) {
	if on {
		*f |= x
	} else {
		*f &^= x
	}
}

// StorageKind is the word type a trace's codes are stored in. (It is not
// called Storage: the HAL's Storage is the board's settings store.)
type StorageKind uint8

// The storage types.
const (
	U8  StorageKind = iota + 1 // Trace.Codes8
	U16                        // Trace.Codes16
)

// Bits returns the storage word's width, or 0 for an unknown storage.
// TRLC-LINKS: REQ-DSO-1003
func (s StorageKind) Bits() uint8 {
	switch s {
	case U8:
		return 8
	case U16:
		return 16
	}
	return 0
}

// MarshalText returns "u8" or "u16".
// TRLC-LINKS: REQ-DSO-1003
func (s StorageKind) MarshalText() ([]byte, error) {
	switch s {
	case U8:
		return []byte("u8"), nil
	case U16:
		return []byte("u16"), nil
	}
	return nil, fmt.Errorf("sample: storage %d has no name", s)
}

// UnmarshalText parses "u8" or "u16".
// TRLC-LINKS: REQ-DSO-1003
func (s *StorageKind) UnmarshalText(b []byte) error {
	switch string(b) {
	case "u8":
		*s = U8
	case "u16":
		*s = U16
	default:
		return fmt.Errorf("sample: unknown storage %q", b)
	}
	return nil
}

// CodeFormat says how stored words map to the converter, separately from
// what the samples are (Content). The SDS1000CML+ stores 8-bit codes as they
// come (Bits 8, U8, Mid 128); a 12-bit converter whose codes sit
// left-aligned in 16-bit words has Bits 12, U16, Shift 4, Mid 0x8000.
type CodeFormat struct {
	Bits    uint8       `yaml:"bits"`    // converter resolution
	Storage StorageKind `yaml:"storage"` // word type
	Shift   uint8       `yaml:"shift"`   // left alignment of the code within its word
	Frac    uint8       `yaml:"frac"`    // fractional bits below the code (fixed-point words)
	Min     uint32      `yaml:"min"`     // lowest word a sample takes
	Max     uint32      `yaml:"max"`     // highest word a sample takes
	Mid     uint32      `yaml:"mid"`     // the word at the screen's centre line
}

// Validate reports a format whose fields cannot describe stored codes.
// TRLC-LINKS: REQ-DSO-1003
func (f CodeFormat) Validate() error {
	sb := f.Storage.Bits()
	switch {
	case sb == 0:
		return errors.New("sample: code format has no storage")
	case f.Bits == 0:
		return errors.New("sample: code format has no bits")
	case int(f.Bits)+int(f.Shift)+int(f.Frac) > int(sb):
		return fmt.Errorf("sample: %d bits shifted %d with %d fraction bits do not fit %d-bit words", f.Bits, f.Shift, f.Frac, sb)
	case f.Max >= 1<<sb:
		return fmt.Errorf("sample: max %d does not fit %d-bit words", f.Max, sb)
	case f.Min > f.Mid || f.Mid > f.Max:
		return fmt.Errorf("sample: want min <= mid <= max, have %d, %d, %d", f.Min, f.Mid, f.Max)
	}
	return nil
}

// Scale converts stored words to input-referred volts at the probe tip:
// v = (word - Zero) * PerCode - OffsetV. It is snapshotted into each trace
// at readout and never read back from the front end afterwards, so a
// setting changed mid-flight cannot re-scale a frame already captured.
type Scale struct {
	Zero    float64 // the word a tip voltage of -OffsetV reads as
	PerCode float64 // volts per word step
	OffsetV float64 // the vertical offset applied in front of the converter
}

// Volts converts one word.
// TRLC-LINKS: REQ-DSO-1003
func (s Scale) Volts(word float64) float64 { return (word-s.Zero)*s.PerCode - s.OffsetV }

// The 8-bit view: every format's words mapped onto the scale of an 8-bit
// converter centred at 128, so kernels written for 8-bit codes and Q8.8
// precision words serve any converter. A word is the format's mid at 128.0;
// one 8-bit step is Unit8 words.

// Mid8 is the 8-bit view's centre line, where every format's Mid lands.
const Mid8 = 128

// q88Shift is how far a word's code moves left to land on the Q8.8 scale.
// TRLC-LINKS: REQ-DSO-1003
func (f CodeFormat) q88Shift() int { return 16 - int(f.Bits) - int(f.Shift) - int(f.Frac) }

// Unit8 is how many words make one step of the 8-bit view: 1 for 8-bit
// codes, 256 for 12-bit codes left-aligned in 16-bit words.
// TRLC-LINKS: REQ-DSO-1003
func (f CodeFormat) Unit8() float64 { return math.Ldexp(1, 8-f.q88Shift()) }

// Q88 maps word w onto the 8-bit view in Q8.8: the format's mid is 0x8000,
// clamped to the 16-bit range.
// TRLC-LINKS: REQ-DSO-1003
func (f CodeFormat) Q88(w uint32) uint16 {
	d := int64(w) - int64(f.Mid)
	if s := f.q88Shift(); s >= 0 {
		d <<= s
	} else {
		d >>= -s
	}
	return uint16(min(max(d+0x8000, 0), 0xFFFF))
}

// Code8 maps word w onto the 8-bit view, rounded: the format's mid is 128.
// TRLC-LINKS: REQ-DSO-1003
func (f CodeFormat) Code8(w uint32) uint8 {
	return uint8(min((uint32(f.Q88(w))+0x80)>>8, 255))
}
