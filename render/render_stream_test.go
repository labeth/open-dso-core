// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import (
	"fmt"
	"testing"

	"github.com/labeth/open-dso-core/streamwire"
)

// streamStatusRef is the fmt-built status line streamStatus must match.
// TRLC-LINKS: REQ-SDS-018
func streamStatusRef(v streamwire.View) string {
	if v.State == streamwire.Starting && v.Note != "" {
		return fmt.Sprintf("STREAM %s  %s  %s", v.Label, v.State, v.Note)
	}
	s := fmt.Sprintf("STREAM %s  %s  %d units  %s/s  err %d  lost %d", v.Label, v.State, v.Units,
		siScale(v.UnitsPerSecond, []siUnit{{1e6, "M"}, {1e3, "k"}, {1, ""}}), v.Errors, v.Lost)
	if v.Mode != "" {
		s += "  " + v.Mode
	}
	if v.Err != "" {
		s += "  " + v.Err
	}
	return s
}

// TRLC-LINKS: REQ-SDS-018
func TestStreamStatusMatchesFmt(t *testing.T) {
	for _, v := range []streamwire.View{
		{Label: "UART 115200", State: streamwire.Streaming, Units: 123456, UnitsPerSecond: 11520, Errors: 2, Lost: 1, Mode: "hex"},
		{Label: "SPI", State: streamwire.Failed, Err: "lost sync", UnitsPerSecond: 0.5},
		{Label: "I2C", State: streamwire.Starting, Note: "loading image"},
		{Label: "I2C", State: streamwire.Starting},
		{Label: "CAN", State: streamwire.Stopped, Units: 1 << 40, UnitsPerSecond: 2.5e6, Errors: 99, Lost: 7},
		{State: streamwire.Idle, UnitsPerSecond: -3},
	} {
		if got, want := streamStatus(v), streamStatusRef(v); got != want {
			t.Errorf("streamStatus = %q, want %q", got, want)
		}
	}
}
