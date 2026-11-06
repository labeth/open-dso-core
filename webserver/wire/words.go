// ENGMODEL-OWNER-UNIT: FU-APP-WEB

package wire

import (
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
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
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
