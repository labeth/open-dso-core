// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package wire

import (
	"encoding/binary"
	"encoding/json"
	"errors"
)

// WordsBinMagic opens a /api/stream/words.bin reply.
var WordsBinMagic = [4]byte{'S', 'D', 'W', '1'}

// WordsHeader is the JSON header of a /api/stream/words.bin reply: the
// stream's description (Info, the scope's: a JSON object whose fields lead
// the header) and the read's place in it.
type WordsHeader struct {
	Info     any
	Next     uint64 // the seq to ask for next
	Dropped  uint64 // chunks asked for that the ring no longer held
	Chunks   int
	Lag      uint64 // stream samples the scope holds without the page's frames
	ServerMS int64
}

// wordsTail is the header's own fields, after Info's.
type wordsTail struct {
	Next     uint64 `json:"next"`
	Dropped  uint64 `json:"dropped"`
	Chunks   int    `json:"chunks"`
	Lag      uint64 `json:"lag"`
	ServerMS int64  `json:"serverMS"`
}

// MarshalJSON writes Info's fields and then the read's, as one object: the
// encoding of a struct that embeds Info.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (h WordsHeader) MarshalJSON() ([]byte, error) {
	tail, err := json.Marshal(wordsTail{Next: h.Next, Dropped: h.Dropped, Chunks: h.Chunks, Lag: h.Lag, ServerMS: h.ServerMS})
	if err != nil {
		return nil, err
	}
	if h.Info == nil {
		return tail, nil
	}
	info, err := json.Marshal(h.Info)
	if err != nil {
		return nil, err
	}
	if len(info) < 2 || info[0] != '{' || info[len(info)-1] != '}' {
		return nil, errors.New("words header: the stream's description is not a JSON object")
	}
	if len(info) == 2 {
		return tail, nil
	}
	out := append(info[:len(info)-1:len(info)-1], ',')
	return append(out, tail[1:]...), nil
}

// AppendWordChunkHeads appends the chunks' heads as /api/stream/words.bin
// carries them before their words: each the index (low and high words),
// the word count, the flags (bit0 a break, bit1 a check) and the coding's
// length, little-endian uint32s.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func AppendWordChunkHeads(dst []byte, chunks []WordChunk) []byte {
	for _, c := range chunks {
		flags := uint32(0)
		if c.Break {
			flags |= 1
		}
		if c.Check {
			flags |= 2
		}
		dst = binary.LittleEndian.AppendUint32(dst, uint32(c.Index)) //nolint:gosec // G115: the index's low word; the high one follows
		dst = binary.LittleEndian.AppendUint32(dst, uint32(c.Index>>32))
		dst = binary.LittleEndian.AppendUint32(dst, uint32(c.Words)) //nolint:gosec // G115: a chunk's word count
		dst = binary.LittleEndian.AppendUint32(dst, flags)
		dst = binary.LittleEndian.AppendUint32(dst, uint32(len(c.Enc))) //nolint:gosec // G115: a chunk's encoding, a few kilobytes
	}
	return dst
}
