// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"
	"unsafe"
)

// ClientLease is how long the client decode lasts after the page's last
// request: a page whose tab is hidden or whose Wi-Fi dropped hands the
// decode back to the scope after it.
const ClientLease = 3 * time.Second

// ClientTapBytes is the ring of coded words the page reads: a second and a
// half of the densest digital stream (2^9, two busy lines), minutes of a
// quiet one.
const ClientTapBytes = 6 << 20

// ClientReplyMax bounds a page's read: a read is written to the page
// straight from the ring and the owner keeps out of it meanwhile, so a read
// holds at most a quarter of the ring.
const ClientReplyMax = ClientTapBytes / 4

// ClientTapChunks bounds the chunks the ring holds (a quiet line's chunk
// is a few bytes).
const ClientTapChunks = 8192

// ClientBitsBudget is the samples a second the digital stream may carry when
// a page decodes it: every period the stream image has, two lines at 64 ns.
const ClientBitsBudget = 32e6

// ClientInboxMax bounds the page's frames queued for the owner.
const ClientInboxMax = 1 << 16

// ClientStreamInfo is what a page needs to decode the stream as the scope
// would: the protocol and its settings, the digital stream's period and the
// stream decoder's sizes (in samples) and the quiet line's (in words).
type ClientStreamInfo struct {
	Session   uint32       `json:"session"` // the stream's identity; a new one restarts the page's decoder
	Proto     string       `json:"proto"`   // the decode package's name
	Params    SerialParams `json:"params"`
	PeriodS   float64      `json:"periodS"` // a sample (a word's sixteenth)
	Log       int          `json:"log"`
	Lines     int          `json:"lines"`
	CloseN    int          `json:"closeN"`
	MaxN      int          `json:"maxN"`
	LeadN     int          `json:"leadN"`
	PushN     int          `json:"pushN"`
	PushMax   int          `json:"pushMax"`
	KeepWords int          `json:"keepWords"`
	LeadWords int          `json:"leadWords"`
	Subframes bool         `json:"subframes"`
	Mask      uint32       `json:"mask"` // the lines' bits in a word; the others are sent as 0
	Active    bool         `json:"active"`
	Why       string       `json:"why,omitempty"` // why the page does not decode, when it does not
}

// ClientChunk is a piece of the stream as the ring holds it: Seq its place
// in the ring's order, Index the stream sample its first word is (the
// scope's decoder index, which the frames posted back are in), Words its
// words and Enc their coding (appendWordsRLE). Break marks words lost
// before it (a worker fault, the decode handed over): the page's decoder
// starts afresh.
type ClientChunk struct {
	Seq   uint64
	Index uint64
	Words int
	Break bool
	Check bool // the scope would decode what it queued after this chunk (softDrain's push check)
	Enc   []byte
	Off   int // Enc's place in the ring
}

// ClientWords is a page's read of the ring: the chunks from its Seq on,
// Dropped those it asked for that the ring no longer holds (the page fell
// behind by the ring), Next the Seq to ask for next.
type ClientWords struct {
	Info    ClientStreamInfo
	Chunks  []ClientChunk
	Dropped uint64
	Next    uint64
	Lag     uint64 // stream samples the scope holds that the page has not posted frames for
}

// ClientFrame is a page's frame as the scope queues it, without a pointer
// (thousands a second, and a pointer each would be the garbage collector's
// work on the device): its spans spans[from:to] (data and errors only, the
// ones the transcript keeps), its start of frame's StartNoteCode, or a loss.
type ClientFrame struct {
	I0, I1   uint64
	From, To int32
	Start    uint32
	Loss     bool
}

// ClientSpan is a frame's span the transcript keeps: its first sample, value
// and kind (0 data, 1 frame error, 2 parity error, 3 gap).
type ClientSpan struct {
	I0   uint64
	Val  int32
	Kind uint8
}

// rleRun is the shortest run of a quiet word the ring codes as a run.
const rleRun = 3

// RLEBound is the most bytes appendWordsRLE writes for n words.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func RLEBound(n int) int { return 4*n + 4*(n/rleRun+1) + 8 }

// AppendWordsRLE appends raw's words (each masked) as tokens: a little-endian
// word k < 2^31 then k words as they are, or 2^31 | k then one word that
// comes k times. Only a quiet word - each line at 0 or 0xFFFF - is coded as
// a run: a bus at rest costs eight bytes, a busy one its words.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func AppendWordsRLE(dst, raw []byte, mask uint32) []byte {
	n := len(raw) / 4
	at := len(dst)
	dst = slices.Grow(dst, RLEBound(n))[:at+RLEBound(n)]
	if n == 0 {
		return dst[:at]
	}
	src, out := words32(raw[:4*n]), words32(dst[at:])
	if src == nil || out == nil {
		return rleBytes(dst, at, raw[:4*n], mask)
	}
	// The words in place, as the device's ring holds them (aligned, the
	// device little-endian like the page): a word costs a load, a mask and
	// two compares, where the bytes' path costs it as much again.
	o := 0
	lit := -1 // the open literal's token
	for i := 0; i < n; {
		w := src[i] & mask
		if (w+1)&0xffff <= 1 && (w>>16+1)&0xffff <= 1 {
			j := i + 1
			for j < n && src[j]&mask == w {
				j++
			}
			if j-i >= rleRun {
				if lit >= 0 {
					out[lit] = uint32(o - lit - 1)
					lit = -1
				}
				out[o], out[o+1] = 1<<31|uint32(j-i), w //nolint:gosec // G115: a run within one chunk of words
				o += 2
				i = j
				continue
			}
		}
		if lit < 0 {
			lit = o
			o++
		}
		out[o] = w
		o++
		i++
	}
	if lit >= 0 {
		out[lit] = uint32(o - lit - 1)
	}
	return dst[:at+4*o]
}

// words32 is b's words in place, nil where they are not aligned or the
// machine is not little-endian.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func words32(b []byte) []uint32 {
	if len(b) < 4 || uintptr(unsafe.Pointer(unsafe.SliceData(b)))%4 != 0 || !littleEndian { //nolint:gosec // G103: the alignment check that makes the view below safe
		return nil
	}
	return unsafe.Slice((*uint32)(unsafe.Pointer(unsafe.SliceData(b))), len(b)/4) //nolint:gosec // G103: an aligned little-endian view of b, checked above
}

// littleEndian is the machine's byte order (the device's and the PC's).
var littleEndian = func() bool {
	x := uint16(1)
	return *(*byte)(unsafe.Pointer(&x)) == 1 //nolint:gosec // G103: reads the first byte of a local uint16
}()

// rleBytes is appendWordsRLE a byte at a time (dst grown, its tokens from
// at), for words that are not aligned.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func rleBytes(dst []byte, at int, raw []byte, mask uint32) []byte {
	n := len(raw) / 4
	o := at
	lit := -1
	litN := uint32(0)
	for i := 0; i < n; {
		w := binary.LittleEndian.Uint32(raw[4*i:]) & mask
		if (w+1)&0xffff <= 1 && (w>>16+1)&0xffff <= 1 {
			j := i + 1
			for j < n && binary.LittleEndian.Uint32(raw[4*j:])&mask == w {
				j++
			}
			if j-i >= rleRun {
				if lit >= 0 {
					binary.LittleEndian.PutUint32(dst[lit:], litN)
					lit = -1
				}
				binary.LittleEndian.PutUint32(dst[o:], 1<<31|uint32(j-i)) //nolint:gosec // G115: a run within one chunk of words
				binary.LittleEndian.PutUint32(dst[o+4:], w)
				o += 8
				i = j
				continue
			}
		}
		if lit < 0 {
			lit, litN = o, 0
			o += 4
		}
		binary.LittleEndian.PutUint32(dst[o:], w)
		o += 4
		litN++
		i++
	}
	if lit >= 0 {
		binary.LittleEndian.PutUint32(dst[lit:], litN)
	}
	return dst[:o]
}

// ClientChunkHead is a chunk's head in a /api/stream/words.bin reply: its
// first word's stream index (two words, low first), its words, its flags
// (1: words lost before it, 2: the scope checks its pushes after it) and its
// coded bytes, each a little-endian word.
const ClientChunkHead = 20

// AppendClientChunkHeads appends the chunks' heads; their coded words follow
// them in a reply, in the same order.
// TRLC-LINKS: REQ-SDS-018, REQ-SDS-240
func AppendClientChunkHeads(dst []byte, chunks []ClientChunk) []byte {
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

// ClientFramesMagic opens a page's frames (webdec.js webdecFramesBody).
var ClientFramesMagic = [4]byte{'S', 'D', 'F', '1'}

// ClientSpanKinds are the span kinds a page's frames carry, by their code:
// the ones the transcript keeps (emit); any other kind as a frame's first
// span, which decides its line; a loss as a frame's only span.
var ClientSpanKinds = []string{SpanData, SpanFrameError, SpanParityError, SpanGap, SpanSOF, "other", "loss"}

// ErrClientFramesBody refuses a body that is not a page's frames.
var ErrClientFramesBody = errors.New("not a client frames body")

// The refusals of a page's frames.
var (
	ErrShortFrames     = fmt.Errorf("%w: cut short", ErrClientFramesBody)
	ErrNotFrames       = ErrClientFramesBody
	ErrNoClientSession = errors.New("no client decode for this session")
	ErrClientQueueFull = errors.New("client frames queue full")
)
