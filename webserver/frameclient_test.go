// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"testing"
	"time"
)

// A page is a frame client while it keeps asking: one that stopped asking
// 5 s ago is not shown as one for 3 s.
// TRLC-LINKS: REQ-DSO-163, REQ-DSO-168
func TestFrameClientExpires(t *testing.T) {
	var s Server
	if s.FrameClient(time.Hour) {
		t.Fatal("a page before any asked for a frame")
	}
	s.frameAt.Store(time.Now().UnixNano())
	if !s.FrameClient(time.Second) {
		t.Fatal("no page right after a frame request")
	}
	s.frameAt.Store(time.Now().Add(-5 * time.Second).UnixNano())
	if s.FrameClient(3 * time.Second) {
		t.Fatal("a page that stopped asking 5 s ago still shows the scope")
	}
}
