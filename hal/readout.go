package hal

import (
	"context"

	"github.com/labeth/open-dso-core/sample"
)

// ReadRequest asks for several windows of the same stored record(s) in one
// pass: a main window beside full-rate detail windows of the same record,
// or a main and a zoom window played out together.
type ReadRequest struct {
	Record   RecordID
	Segments [2]int // [first, last); {0, 1} for one record
	Windows  []Window
}

// Window is one span of a stored record to deliver.
type Window struct {
	Role        Role
	Channels    []int // a board that reads every acquired channel discards the rest
	StartSample int64 // acquired samples, relative to the trigger
	// Span is the acquired samples covered. 0: the board's own main
	// window (StartSample unused), at least the plan's screen
	// (WinSamples) where the request placed it about the trigger; a board
	// may deliver its whole record.
	Span int64
	// Points is how many samples to deliver; Span/Points is the readout
	// compression. 0: what the board delivers for Span (a board that cannot
	// resample to any count), reported back in the frame. For PeakPairs it
	// counts buckets (each two codes); fewer buckets than the record holds
	// merge whole pairs, the minimum of the minima and the maximum of the
	// maxima, so a board whose hardware compression keeps every Nth word
	// (and would split pairs) reads peak windows uncompressed and merges
	// them itself.
	Points int
	Want   Content
}

// ReadLimits says what one Read can do.
type ReadLimits struct {
	MaxPoints         int // per window
	MaxWindows        int
	MaxSegments       int
	ChannelsPerWindow int // 1: the core issues one window per channel
}

// Readout gets data out of the record memory into core-owned frames, so a
// board never allocates per frame.
type Readout interface {
	// Read fills dst[i] for each window × segment, row-major (windows
	// vary fastest), each frame sized by the caller or left for the board
	// to size. A frame leaves the board conditioned: its calibration and
	// its filters applied and stated in Frame.Cond, so the core only
	// filters further what the user asks. A window whose Want the board
	// cannot deliver is a
	// *Refusal, never another content kind. A Main window that does not
	// fill its frame is an error; any other window's frame is cut to what
	// it got, so a detail window that misses does not cost the frame.
	// yield is called between chunks of a long read; ctx ends a read
	// nobody wants any more. OWNER, only in AcqState Ready (or Idle with a
	// stored record).
	Read(ctx context.Context, req ReadRequest, dst []*sample.Frame, yield func()) error
	// Export delivers one channel's raw codes through the board's slow path.
	// LONG.
	Export(ctx context.Context, rec RecordID, ch int, dst *sample.Trace) error
	// Limits says what one Read can do. ANY.
	Limits() ReadLimits
}

// ScreenRequest asks for the window a board reads of its stored record to
// show the armed plan's screen while running: the screen with the trigger
// at PosFrac of it, PreRoom acquired samples more before it (room for a
// trigger point placed back in software, a protocol match's start), the
// margins the board's reads need, and, with Envelope, compressed to the
// board's display envelope (peak pairs) where the window holds more
// samples than the display needs.
type ScreenRequest struct {
	PosFrac  float64
	PreRoom  int
	Envelope bool
}

// ScreenReader is optional on a Readout: the screen window of the stored
// record (ScreenRequest), as a Window the owner reads with Read, beside
// the detail windows it plans from it. ANY on the owner's goroutine, in
// AcqState Ready.
type ScreenReader interface {
	ScreenWindow(r ScreenRequest) (Window, error)
}
