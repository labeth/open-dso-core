// ENGMODEL-OWNER-UNIT: FU-DSO-PANEL

package panel

import (
	"context"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
)

// The board's own stream sessions (hal.Session) as the UI model drives
// them on a HAL scope: the SDS1000CML+'s decoded streams answer the same
// calls its engine answered - the scope's keys end a web page's decoded
// capture, a stream started again holds its image, and the snapshot says
// how the stream runs and whether a page's capture holds the acquisition.

// sessionStatus is a session that says how its stream runs.
type sessionStatus interface {
	DecodedStatus() (mode string, web bool)
}

// session is the board's stream sessions, nil without.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func (e halEngine) session() hal.Session {
	if e.b == nil {
		return nil
	}
	s, _ := hal.SessionOf(e.b)
	return s
}

// fillSessionStatus puts the board's decoded stream's mode and a web
// page's capture into st.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-018
func fillSessionStatus(s hal.Session, st *instrument.Stats) {
	if x, ok := s.(sessionStatus); ok {
		st.DecodedStreamMode, st.DecodedWeb = x.DecodedStatus()
	}
}

// EndWebDecodedCapture ends a web page's decoded capture the board's
// session holds, for the scope's run-control keys; false: none was open.
// TRLC-LINKS: REQ-DSO-013, REQ-DSO-212
func (e halEngine) EndWebDecodedCapture(ctx context.Context) (bool, error) {
	if x, ok := e.session().(interface {
		EndWebDecodedCapture(context.Context) (bool, error)
	}); ok {
		return x.EndWebDecodedCapture(ctx)
	}
	return false, nil
}

// HoldDecodedImage keeps the board's image while a decoded stream starts
// again; nothing on a board without its own sessions.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-228
func (e halEngine) HoldDecodedImage(ctx context.Context) error {
	if x, ok := e.session().(interface {
		HoldDecodedImage(context.Context) error
	}); ok {
		return x.HoldDecodedImage(ctx)
	}
	return nil
}
