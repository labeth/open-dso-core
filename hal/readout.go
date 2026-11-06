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
	Span        int64 // acquired samples covered
	// Points is how many samples to deliver; Span/Points is the readout
	// compression. 0: what the board delivers for Span (a board that cannot
	// resample to any count), reported back in the frame.
	Points int
	Want   Content
}

// Role is what a window is for.
type Role int

// The roles. RoleView(n) numbers the magnified views' detail windows.
const (
	Main    Role = iota // the screen's window
	Zoom                // a zoom window beside the main one
	Edge                // full-rate window around the trigger edge
	EdgeOpp             // full-rate window around the first opposite edge after it
	view0               // first View role
)

// RoleView is the n-th magnified view's detail window. (It is not called
// View: that is the hardware renderer's view.)
func RoleView(n int) Role { return view0 + Role(n) }

// ViewIndex returns n for RoleView(n), or false for another role.
func (r Role) ViewIndex() (int, bool) { return int(r - view0), r >= view0 }

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
	// vary fastest). A window whose Want the board cannot deliver is a
	// *Refusal, never another content kind. yield is called between chunks
	// of a long read. OWNER, only in AcqState Ready (or Idle with a stored
	// record).
	Read(req ReadRequest, dst []*sample.Frame, yield func()) error
	// Export delivers one channel's raw codes through the board's slow path.
	// LONG.
	Export(ctx context.Context, rec RecordID, ch int, dst *sample.Trace) error
	// Limits says what one Read can do. ANY.
	Limits() ReadLimits
}
