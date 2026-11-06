// ENGMODEL-OWNER-UNIT: FU-APP-WEB

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
	Err     string  `json:"err,omitempty"`
}

// EncodeRecord lays out /api/record.bin: a little-endian uint32 header
// length, the JSON header, then the codes, C1's then C2's.
// TRLC-LINKS: REQ-SDS-280
func EncodeRecord(h RecordReply, c1, c2 []byte) []byte {
	js, _ := json.Marshal(h)
	out := make([]byte, 4, 4+len(js)+len(c1)+len(c2))
	binary.LittleEndian.PutUint32(out, uint32(len(js))) //nolint:gosec // G115: a JSON header of a few KB
	out = append(out, js...)
	out = append(out, c1...)
	return append(out, c2...)
}
