// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER

package render

import (
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/streamwire"
)

// A stream that waits for an FPGA image says why instead of its (empty)
// counts; once it streams, the counts.
// TRLC-LINKS: REQ-DSO-018
func TestStreamStatusSaysWhyItWaits(t *testing.T) {
	note := "loading the general FPGA image, about 4 s: the qualifier image has no UART receiver"
	s := streamStatus(streamwire.View{State: streamwire.Starting, Label: "UART 115200 8N1 on C1", Note: note})
	if s != "STREAM UART 115200 8N1 on C1  START  "+note || TextWidth(s, 1) > W-12 {
		t.Fatalf("starting: %q (%d px)", s, TextWidth(s, 1))
	}
	if s := streamStatus(streamwire.View{State: streamwire.Streaming, Label: "UART", Units: 5}); !strings.Contains(s, "5 units") {
		t.Fatalf("streaming: %q", s)
	}
}
