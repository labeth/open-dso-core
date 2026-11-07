package session

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/labeth/open-dso-core/analysis"
	"github.com/labeth/open-dso-core/framecalc"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/uimodel"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// frameWaitMs is how long a frame request waits for a frame after the one
// it names.
const frameWaitMs = 2000

// rawFrame reads the scope's current record as it serves it for a page's
// own decoding: a v2 raw frame where the scope serves v2, else the v1 raw
// shape (two channels of 8-bit codes or Q8.8 words).
// TRLC-LINKS: REQ-DSO-295
func rawFrame(ctx context.Context, c *Client, since uint64) (*sample.Frame, error) {
	b, _, err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/api/frame.bin?v=2&raw=1&full=1&since=%d&waitms=%d", since, frameWaitMs), nil)
	if err != nil {
		return nil, err
	}
	if len(b) >= 4 && string(b[:4]) == string(wire.FrameMagic2[:]) {
		return frameOfV2(b)
	}
	return frameOfV1Raw(b)
}

// frameOfV2 is a v2 raw frame as a sample frame: one trace per channel,
// in channel order.
// TRLC-LINKS: REQ-DSO-295
func frameOfV2(b []byte) (*sample.Frame, error) {
	wf, err := wire.DecodeFrame2(b)
	if err != nil {
		return nil, err
	}
	h := wf.Header
	f := &sample.Frame{SampleS: h.SampleS, TriggerAt: h.TriggerAt, Seq: h.Seq}
	n := 0
	for _, t := range wf.Traces {
		for len(f.Traces) <= t.Ch {
			f.Traces = append(f.Traces, sample.Trace{Ch: len(f.Traces)})
		}
		tr := &f.Traces[t.Ch]
		tr.Format = sampleFormat(t.Fmt)
		tr.Codes8, tr.Codes16 = t.Codes.U8, t.Codes.U16
		tr.Valid = t.Codes.Len()
		if n == 0 || (tr.Valid > 0 && tr.Valid < n) {
			n = tr.Valid
		}
	}
	if n == 0 {
		return nil, errors.New("the raw frame holds no samples")
	}
	f.SetLen(n)
	return f, nil
}

// sampleFormat is a wire code format as package sample's.
// TRLC-LINKS: REQ-DSO-295
func sampleFormat(w wire.CodeFormat) sample.CodeFormat {
	var st sample.StorageKind
	_ = st.UnmarshalText([]byte(w.Storage)) // an unknown storage leaves none: the trace then has no codes to read
	return sample.CodeFormat{Bits: w.Bits, Storage: st, Shift: w.Shift, Frac: w.Frac, Min: w.Min, Max: w.Max, Mid: w.Mid}
}

// v1RawHeader is what the v1 raw shape's header says of its record.
type v1RawHeader struct {
	Seq     uint64  `json:"seq"`
	SampleS float64 `json:"sample_s"`
	EdgeX   float64 `json:"edge_x"`
	DtS     float64 `json:"dt_s"`
}

// frameOfV1Raw is a v1 raw frame (c1[n] c2[n], 8-bit codes or Q8.8
// words) as a two-channel 8-bit sample frame.
// TRLC-LINKS: REQ-DSO-295
func frameOfV1Raw(b []byte) (*sample.Frame, error) {
	if len(b) < 8 || b[0] != wire.BinMagic {
		return nil, errors.New("not a frame")
	}
	flags := b[1]
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	if 8+n > len(b) {
		return nil, errors.New("a short frame header")
	}
	if flags&wire.FlagRaw == 0 || flags&(wire.FlagEmpty|wire.FlagUnchanged) != 0 {
		return nil, errors.New("no raw record served")
	}
	var h v1RawHeader
	if err := json.Unmarshal(b[8:8+n], &h); err != nil {
		return nil, err
	}
	payload := b[8+n:]
	f := &sample.Frame{SampleS: h.SampleS, TriggerAt: h.EdgeX, Seq: h.Seq}
	if f.SampleS == 0 {
		f.SampleS = h.DtS
	}
	format := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	var codes [2][]uint8
	if flags&wire.FlagPrecision != 0 {
		m := len(payload) / 4
		for ch := range 2 {
			codes[ch] = make([]uint8, m)
			for i := range m {
				w := binary.LittleEndian.Uint16(payload[2*(ch*m+i):])
				codes[ch][i] = uint8(min((uint32(w)+0x80)>>8, 255))
			}
		}
	} else {
		m := len(payload) / 2
		codes = [2][]uint8{payload[:m], payload[m : 2*m]}
	}
	for ch := range 2 {
		f.Traces = append(f.Traces, sample.Trace{Ch: ch, Format: format, Codes8: codes[ch]})
	}
	f.SetLen(len(codes[0]))
	if f.Len() == 0 {
		return nil, errors.New("the raw frame holds no samples")
	}
	return f, nil
}

// decodePackets decodes f with the panel's decode settings as the scope's
// packet list does, and returns each packet as text.
// TRLC-LINKS: REQ-DSO-295
func decodePackets(f *sample.Frame, d uimodel.Display) []string {
	p := framecalc.Params(d.DecProto, d.DecBaud, d.DecChA, d.DecChB, d.DecCPOL, d.DecCPHA, d.DecLSB, d.DecFormat, d.DecDelayNs, d.DecParity)
	if d.DecProto == framecalc.ProtoUART {
		p.Parity, p.Inverted = d.DecParity, d.DecInverted
	}
	valid := framecalc.Valid(f)
	res := framecalc.Decode(f, p)
	var out []string
	for _, pk := range analysis.Packets(res, valid) {
		out = append(out, packetText(pk))
	}
	return out
}

// packetText is one packet as text: its address, its bytes in hex and,
// when they are printable, as quoted text; a cut packet is marked.
// TRLC-LINKS: REQ-DSO-295
func packetText(p analysis.Packet) string {
	var b strings.Builder
	if p.HasAddr {
		rw := "W"
		if p.Read {
			rw = "R"
		}
		fmt.Fprintf(&b, "@%02X%s ", p.Addr, rw)
	}
	b.WriteString(hex.EncodeToString(p.Bytes))
	if printable(p.Bytes) {
		fmt.Fprintf(&b, " %q", string(p.Bytes))
	}
	if !p.Complete {
		b.WriteString(" (cut)")
	}
	if p.Err != "" {
		b.WriteString(" err:" + p.Err)
	}
	return b.String()
}

// printable reports whether b is non-empty printable ASCII.
// TRLC-LINKS: REQ-DSO-295
func printable(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return len(b) > 0
}

// packetBytesText is every complete packet's bytes run together, with
// unprintable bytes as \xNN, so an expectation can look for a message.
// TRLC-LINKS: REQ-DSO-295
func packetBytesText(f *sample.Frame, d uimodel.Display) string {
	p := framecalc.Params(d.DecProto, d.DecBaud, d.DecChA, d.DecChB, d.DecCPOL, d.DecCPHA, d.DecLSB, d.DecFormat, d.DecDelayNs, d.DecParity)
	if d.DecProto == framecalc.ProtoUART {
		p.Parity, p.Inverted = d.DecParity, d.DecInverted
	}
	var b strings.Builder
	for _, pk := range analysis.Packets(framecalc.Decode(f, p), framecalc.Valid(f)) {
		for _, c := range pk.Bytes {
			if c >= 0x20 && c <= 0x7e {
				b.WriteByte(c)
			} else {
				fmt.Fprintf(&b, `\x%02x`, c)
			}
		}
	}
	return b.String()
}
