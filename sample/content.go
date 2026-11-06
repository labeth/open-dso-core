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
	"strings"
)

// Content is what a frame's samples are, independent of how they are stored.
type Content uint8

// The content kinds. The zero value is unknown: a frame that does not say
// what it holds must not be fed to a consumer that needs a particular kind.
const (
	RawFullRate   Content = iota + 1 // every converter sample at the acquisition rate
	RawDecimated                     // every Nth sample, or a filtered decimation
	PeakPairs                        // min/max per bucket (Trace.Min*/Max*)
	Resampled                        // interpolated or resampled to N points
	DisplayDomain                    // already in a display code space
)

var contentNames = [...]string{"", "raw-full-rate", "raw-decimated", "peak-pairs", "resampled", "display-domain"}

// String returns the kind's profile name, or "unknown".
func (c Content) String() string {
	if int(c) < len(contentNames) && c != 0 {
		return contentNames[c]
	}
	return "unknown"
}

// MarshalText returns the kind's profile name.
func (c Content) MarshalText() ([]byte, error) {
	if c == 0 || int(c) >= len(contentNames) {
		return nil, fmt.Errorf("sample: content %d has no name", c)
	}
	return []byte(contentNames[c]), nil
}

// UnmarshalText parses a profile name such as "peak-pairs".
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
func ContentSetOf(cs ...Content) ContentSet {
	var s ContentSet
	for _, c := range cs {
		s |= 1 << c
	}
	return s
}

// Has reports whether c is in s.
func (s ContentSet) Has(c Content) bool { return c != 0 && s&(1<<c) != 0 }

// String lists the set's names, comma separated.
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
func (s ContentSet) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText parses comma-separated names; empty text is the empty set.
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
	Ordered   ContentFlag = 1 << iota // peak pairs in time order: decodable as samples at SampleS
	Interp                            // the consumer should interpolate between samples
	Roll                              // a scrolling roll view, newest sample last, untriggered
	Held                              // a held frame shown while stopped
	Segmented                         // one of several segments of a record
	Precision                         // traces carry Precision words beside their codes
)

// StorageKind is the word type a trace's codes are stored in. (It is not
// called Storage: the HAL's Storage is the board's settings store.)
type StorageKind uint8

// The storage types.
const (
	U8  StorageKind = iota + 1 // Trace.Codes8
	U16                        // Trace.Codes16
)

// Bits returns the storage word's width, or 0 for an unknown storage.
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
func (s Scale) Volts(word float64) float64 { return (word-s.Zero)*s.PerCode - s.OffsetV }
