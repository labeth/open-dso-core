// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// DecodeFrame2 reads a wire v2 frame as the page does: the header, and
// each trace's segments by role (the raw role into Codes). The words are
// copied out of b.
// TRLC-LINKS: REQ-DSO-163
func DecodeFrame2(b []byte) (Frame, error) {
	var f Frame
	if len(b) < 8 || string(b[:4]) != string(FrameMagic2[:]) {
		return f, errors.New("wire: not a v2 frame")
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	if 8+n > len(b) {
		return f, fmt.Errorf("wire: a header of %d bytes in %d", n, len(b))
	}
	if err := json.Unmarshal(b[8:8+n], &f.Header); err != nil {
		return f, err
	}
	start := align8(8 + n)
	for _, th := range f.Header.Traces {
		f.Traces = append(f.Traces, FrameTrace{TraceHeader: th})
	}
	for _, s := range f.Header.Segs {
		at := start + s.At
		size := s.N * storageBytes(s.Storage)
		if s.At < 0 || s.N < 0 || at+size > len(b) {
			return f, fmt.Errorf("wire: segment %+v outside the payload", s)
		}
		var w Samples
		if s.Storage == "u16" {
			w.U16 = make([]uint16, s.N)
			for i := range w.U16 {
				w.U16[i] = binary.LittleEndian.Uint16(b[at+2*i:])
			}
		} else {
			w.U8 = append([]uint8(nil), b[at:at+size]...)
		}
		i := -1
		for k := range f.Traces {
			if f.Traces[k].Ch == s.Ch {
				i = k
			}
		}
		if i < 0 {
			return f, fmt.Errorf("wire: a segment of channel %d without its trace", s.Ch)
		}
		t := &f.Traces[i]
		switch s.Role {
		case RoleCodes, RoleRaw:
			t.Codes = w
		case RoleMin:
			t.Min = w
		case RoleMax:
			t.Max = w
		case RoleView:
			t.View = w
		}
	}
	return f, nil
}

// Volts is word i of samples w in volts at the probe tip, by the trace's
// scale.
// TRLC-LINKS: REQ-DSO-163
func (t *TraceHeader) Volts(w Samples, i int) float64 {
	var word float64
	if len(w.U16) > 0 {
		word = float64(w.U16[i])
	} else {
		word = float64(w.U8[i])
	}
	return (word-t.Scale.Zero)*t.Scale.PerCodeV - t.Scale.OffV
}
