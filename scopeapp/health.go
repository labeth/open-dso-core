// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"fmt"
	"os"
	"time"

	"github.com/labeth/open-dso-core/instrument"
)

// health is what the health writer reads: the instrument's counters and a
// heartbeat that moves while the owner loop runs.
type health struct {
	snap  func() instrument.Stats
	beats func() uint64
}

// writeHealth writes the supervisor's health token at path while the owner
// loop beats, until stop. The token says what the app attests: "diag" once
// it runs (the fabric verified, the engine alive), "frames" after three
// coherent frames. A wedged engine stops it, so the supervisor rolls the
// app back. The agent reads the line as it is (its format is the
// app-supervisor contract).
// TRLC-LINKS: REQ-DSO-294
func writeHealth(path string, h health, stop <-chan struct{}, logf func(string, ...any), mark func(string)) {
	var lastBeats uint64
	var lastWrite time.Time
	mode := "off"
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		s := h.snap()
		if s.Wedged {
			continue
		}
		want := "diag"
		if s.Coherent >= 3 && s.Frames >= 3 {
			want = "frames"
		}
		if want != mode {
			mode = want
			if mode == "frames" {
				logf("health: %d coherent frames — health token attests FRAMES", s.Coherent)
				mark("frames")
			} else {
				logf("health: DIAG HEARTBEAT — fabric verified and engine alive, no coherent frames yet; token tagged mode=diag")
			}
		}
		beats := h.beats()
		if beats == lastBeats || time.Since(lastWrite) < 450*time.Millisecond {
			continue
		}
		tok := fmt.Sprintf("mode=%s frames=%d beats=%d coherent=%d published=%d ts=%d\n",
			mode, s.Frames, beats, s.Coherent, s.Published, time.Now().UnixNano())
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(tok), 0o644); err == nil { //nolint:gosec // G306: the supervisor reads the token
			if os.Rename(tmp, path) == nil { //nolint:gosec // G703: the path is the app's own health token, never a client's
				lastBeats, lastWrite = beats, time.Now()
			}
		}
	}
}
