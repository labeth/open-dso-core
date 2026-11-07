// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"encoding/binary"
	"encoding/json"
)

// RecordReply is /api/record.bin's header: the record's samples a channel,
// their interval, and how a code reads as volts on each channel (off is the
// offset applied, vpc the volts a code).
type RecordReply struct {
	Seq     uint64  `json:"seq"`
	N       int     `json:"n"`
	SampleS float64 `json:"sample_s"`
	TdivS   float64 `json:"tdiv_s"`
	Vpc1    float64 `json:"vpc1"`
	Vpc2    float64 `json:"vpc2"`
	Off1V   float64 `json:"off1_v"`
	Off2V   float64 `json:"off2_v"`
	// Chans names the channels the codes are of, in their order, with each
	// one's scale, when they are not C1 then C2.
	Chans []int     `json:"chans,omitempty"`
	Vpc   []float64 `json:"vpc,omitempty"`
	Off   []float64 `json:"off_v,omitempty"`
	Err   string    `json:"err,omitempty"`
}

// EncodeRecord lays out /api/record.bin: a little-endian uint32 header
// length, the JSON header, then the codes, C1's then C2's.
// TRLC-LINKS: REQ-DSO-280
func EncodeRecord(h RecordReply, c1, c2 []byte) []byte { return EncodeRecordN(h, c1, c2) }

// EncodeRecordN lays out a record of any channels: the header length, the
// JSON header, then each channel's codes in turn (the header's chans order).
// TRLC-LINKS: REQ-DSO-280
func EncodeRecordN(h RecordReply, codes ...[]byte) []byte {
	js, _ := json.Marshal(h)
	n := 4 + len(js)
	for _, c := range codes {
		n += len(c)
	}
	out := make([]byte, 4, n)
	binary.LittleEndian.PutUint32(out, uint32(len(js))) //nolint:gosec // G115: a JSON header of a few KB
	out = append(out, js...)
	for _, c := range codes {
		out = append(out, c...)
	}
	return out
}
