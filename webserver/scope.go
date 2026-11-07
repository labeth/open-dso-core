// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/labeth/open-dso-core/streamwire"
	"github.com/labeth/open-dso-core/uimodel"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// Scope is what a scope app hands to New. Status, Frames and Settings are
// required; every other part is optional by typed accessor: ok false means
// the scope lacks it, and its routes answer as a scope without it does (404,
// or the refusal the route names). Every method may be called from any
// handler goroutine: a scope stages changes to its acquisition's owner and
// never touches hardware from a handler.
type Scope interface {
	// Status is the /api/status document, encoded as JSON as it is.
	Status() any
	Frames() Frames
	Settings() Settings

	UI() (UIModel, bool)                  // /api/panel, /api/panel/wait, /api/sheets*, /api/stream/page
	Refs() (Refs, bool)                   // /api/refs
	Stream() (Stream, bool)               // /api/stream/{lines,lines.bin,window}
	Records() (Records, bool)             // /api/record.bin
	Triggers() (Triggers, bool)           // /api/zones, /api/serial, /api/serial/protocols
	Masks() (Masks, bool)                 // /api/mask, /api/maskfail
	Bode() (Bode, bool)                   // /api/bode
	Sound() (Sound, bool)                 // /api/sound.bin, /api/sound/take.bin
	SoundControls() (SoundControls, bool) // /api/sound
	ClientDecode() (ClientDecode, bool)   // /api/stream/{words.bin,frames.bin}
	Setups() (Setups, bool)               // /api/setup
	Screen() (Screen, bool)               // /api/screen.png
	// Extensions are the scope's own routes, beside the shared API.
	Extensions() []Route
}

// Route is a scope-only endpoint, mounted beside the shared routes.
type Route struct {
	Pattern string // a ServeMux pattern, e.g. "POST /api/superres/fpga"
	Feature string // the scope feature it belongs to
	Handler http.Handler
}

// Frames serves the scope's published frames over /api/frame.bin.
type Frames interface {
	// Wait parks until a frame after since has published, or timeout.
	Wait(since uint64, timeout time.Duration)
	// Frame builds the reply for q under the scope's frame lock; the reply
	// owns its data (no lock is held when it is encoded and written).
	Frame(q FrameQuery) wire.FrameReply
	// RawFrame builds the raw shape (?raw=1): the un-windowed, pre-coupling
	// record, or an unchanged reply when no frame after since published.
	RawFrame(since uint64) wire.RawFrame
	// Lease keeps a consumer's need alive (Hold 0 ends it). false: the
	// scope has no such need to lease; the request goes on as a plain poll.
	Lease(n Need) bool
}

// Quiet is optional on Frames: a scope whose acquisition is disturbed by
// CPU bursts (a single core during settle and drain) brackets the
// building and encoding of a reply with it.
type Quiet interface {
	QuietRLock()
	QuietRUnlock()
}

// FrameQuery is a page's frame request.
type FrameQuery struct {
	Since uint64 // the frame the page holds: an unchanged reply when no newer one
	Cols  int    // screen columns, 64..4096
	Full  bool   // the whole record where it is deeper than the screen; the page windows it
}

// The needs a page leases with lease= (frame.bin), each kept alive by its
// polls and given back by lease=…&end=1.
const (
	NeedRaw      = "raw"      // full-rate samples (eye, super-res, a bus search's record)
	NeedSpectrum = "spectrum" // the frames a spectrum is taken of (FFT, spectrogram)
	NeedSamples  = "samples"  // evenly sampled frames (a bus search)
	NeedSearch   = "search"   // the whole records a bus search reads at slow timebases
	NeedView     = "view"     // the page's zoom into a live envelope at full detail
)

// Need is a consumer's requirement on the frames it is served.
type Need struct {
	Kind string        // one of the Need* kinds
	Hold time.Duration // how long it holds without another poll; 0 gives it back
	View ViewWindow    // NeedView: the stretch, zero to end it
}

// ViewWindow is the page's zoom into a live envelope: screen widths from
// the trigger (A..B) and the canvas columns.
type ViewWindow struct {
	A, B float64
	Cols int
}

// Settings applies the page's scalar controls (/api/set).
type Settings interface {
	// Set applies one control; ctx is the request's.
	Set(ctx context.Context, c Control) Applied
}

// Control is one /api/set request: the control's name, its value and the
// trigger qualifiers' parameters.
type Control struct {
	Name  string  `json:"control"`
	Value float64 `json:"value"`
	Lvl   float64 `json:"lvl"`  // pulse level fraction 0..1
	Lo    float64 `json:"lo"`   // slope low fraction
	Hi    float64 `json:"hi"`   // slope high fraction
	Min   float64 `json:"min"`  // width/time window min, ns
	Max   float64 `json:"max"`  // width/time window max, ns
	Cond  int     `json:"cond"` // 0=any 1=less 2=greater 3=inside
	Std   int     `json:"std"`  // video: 0=PAL 1=NTSC
	Line  int     `json:"line"` // video: 0=any, else line N
	Neg   bool    `json:"neg"`  // video: negative sync
}

// Applied is what a control did: the reply {"ok","applied","err"}.
type Applied struct {
	OK    bool
	Value any    // the value applied; nil: the reply has none
	Err   string // why it was refused
	// Brief: the reply carries ok and applied only (a control that
	// cannot be refused).
	Brief bool
}

// UIModel is the scope's UI model as the page and the phone drive it: the
// keys and knobs of its panel. The value may also implement UIRemote,
// UIWorkspaces, UIAutoFor, UIKeys, UIPageStream and UIPageLeave.
type UIModel interface {
	InjectButton(name string) bool
	InjectKnob(name string, dir, steps int) bool
}

// UIRemote is the remote surface: the panel snapshot with its long-poll, the
// settings sheets, setting a row or running an action by id, and typing
// into the display's current context.
type UIRemote interface {
	PanelStatus() uimodel.Status
	WaitPanel(seq uint64, timeout time.Duration) uimodel.Status
	Sheets() uimodel.SheetsModel
	SheetSet(row string, value any, text string) uimodel.SetResult
	SheetAction(row, action string) uimodel.SetResult
	TypeText(text string) uimodel.SetResult
}

// UIStatus is the panel snapshot alone.
type UIStatus interface {
	PanelStatus() uimodel.Status
}

// UIWorkspaces is the remote's workspace switch and the active
// workspace's "again".
type UIWorkspaces interface {
	RemoteWorkspace(name string) uimodel.SetResult
	RemoteAgain(want string) uimodel.SetResult
}

// UIAutoFor presses AUTO for a named workspace (without it AUTO frames for
// the display's workspace).
type UIAutoFor interface {
	InjectAutoFor(ws string) bool
}

// UIKeys is the remote's keys and knobs, answered once they have run with
// what they did (without it a key is only queued).
type UIKeys interface {
	RemoteKey(name string, nomenu bool) uimodel.SetResult
	RemoteKnob(name string, dir, steps int, nomenu bool) uimodel.SetResult
}

// UIPageStream starts or ends the scope's decoded stream for a page.
type UIPageStream interface {
	PageStream(on bool) uimodel.SetResult
}

// UIPageLeave is a page going: only a stream it still holds ends.
type UIPageLeave interface {
	PageLeave() uimodel.SetResult
}

// Refs is the scope's saved references (REF A and B) as the page shows
// them. The value may also implement RefActions.
type Refs interface {
	RefsExport() [2]uimodel.RefExport
}

// RefActions stores, shows, hides or deletes a reference slot.
type RefActions interface {
	RefAction(slot int, op string) uimodel.SetResult
}

// Stream is the scope's decoded stream transcript. ok false: no stream now.
type Stream interface {
	StreamLines(from, limit int) (streamwire.LinesPage, bool)
	StreamLinesBinary(from int, dst []byte) (int, bool)
	StreamWindow(rows int) (streamwire.Page, bool)
}

// Records takes one whole sampled record captured after the call. The
// server allows one at a time.
type Records interface {
	// Take waits up to wait for a record sampled every sampleS or finer; an
	// error is the refusal, its text the reason.
	Take(ctx context.Context, sampleS float64, wait time.Duration) (wire.Record, error)
}

// Triggers is the scope's zone and protocol triggers.
type Triggers interface {
	SetZones(z []wire.Zone)
	// SerialConfig is the protocol trigger as armed and its mode; ok false
	// when it cannot be read back.
	SerialConfig() (cfg any, mode int, ok bool)
	// SetSerial reads the configuration with decode (into the scope's own
	// type), bounds it and installs it; an error is decode's.
	SetSerial(decode func(v any) error) error
	// SerialProtocols lists the protocols the trigger takes.
	SerialProtocols() any
}

// Masks is the envelope mask test.
type Masks interface {
	SetMask(m wire.Mask)
	// ClearMask removes the mask with its test and counts.
	ClearMask()
	// MaskGen names the mask installed: each install or clear counts one.
	MaskGen() uint64
	MaskFails() []wire.MaskFail
	// VertScales are each channel's offset volts and volts per code now.
	VertScales() (off, vpc [2]float64)
}

// Bode is the frequency-response curve.
type Bode interface {
	BodePoints() []wire.BodePoint
}

// Sound is the scope's audio stream. The value may also implement
// SoundTakes.
type Sound interface {
	// Lease holds the stream for a page (who: "phone" or ""); "" when held,
	// else the state a paused page shows (the display holds the acquisition).
	Lease(who string) (paused string)
	Info() wire.SoundInfo
	Read(from uint64, limit int) wire.SoundBlock
	Wait(after uint64, timeout time.Duration)
}

// SoundTakes is the stream's takes (STOP, or a NORMAL or SINGLE trigger's).
type SoundTakes interface {
	// Take is the take shown; nil with none.
	Take() *wire.SoundTake
}

// SoundControls is SOUND's settings, actions and the scope's readouts.
type SoundControls interface {
	SoundSettings() uimodel.SoundSettings
	SetSoundSettings(uimodel.SoundSettings) uimodel.SoundSettings
	SoundAction(name string) error
	// SoundReport is the readouts (encoded NaN-safe); api asks the scope to
	// keep its own analysis current, marks the FPGA's edge marks.
	SoundReport(api, marks bool) any
	SoundWant(api, marks bool)
}

// ClientDecode hands a page the digital stream's words to decode and takes
// its frames back.
type ClientDecode interface {
	// ReadWords reads the stream's chunks from seq, up to limit bytes,
	// parking up to wait; release returns the chunks' memory once written.
	ReadWords(seq uint64, limit int, wait time.Duration, dst []wire.WordChunk) (w wire.Words, release func())
	// ReplyMax bounds a read.
	ReplyMax() int
	// PostFrames queues a page's decoded frames; a BadRequest error is a
	// body that is not frames.
	PostFrames(body []byte) error
}

// Setups saves and recalls whole instrument setups in numbered slots. The
// value may also implement SetupLister.
type Setups interface {
	SaveSetup(slot int) error
	RecallSetup(slot int) error
}

// SetupLister says what the setup slots hold.
type SetupLister interface {
	SetupSlots() []wire.SetupSlot
}

// Screen is the device screen.
type Screen interface {
	PNG() []byte
}

// BadRequest marks an error as the request's fault (400), not the scope's
// state (409). Its text is the error's.
type BadRequest struct{ Err error }

// Error is the wrapped error's text.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (e BadRequest) Error() string { return e.Err.Error() }

// Unwrap is the wrapped error.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func (e BadRequest) Unwrap() error { return e.Err }

// isBadRequest reports a BadRequest anywhere in err's chain.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-240
func isBadRequest(err error) bool {
	var b BadRequest
	return errors.As(err, &b)
}
