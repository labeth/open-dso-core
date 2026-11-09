// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb_test

import (
	"testing"

	"github.com/labeth/open-dso-core/engine/fanout"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/scopeweb"
	"github.com/labeth/open-dso-core/webserver"
)

// countQuiet counts the quiet lock's holds.
type countQuiet struct{ held, taken int }

func (q *countQuiet) QuietRLock()   { q.held++; q.taken++ }
func (q *countQuiet) QuietRUnlock() { q.held-- }

// A scope given a quiet lock serves frames that take it, on the v1 wire
// and on a board v1 cannot carry alike; without one its frames take none.
// TRLC-LINKS: REQ-DSO-163
func TestFramesTakeTheQuietLock(t *testing.T) {
	for _, c := range []struct {
		name  string
		chans int
		v2    bool
	}{{"v1 wire", 2, false}, {"v2 only", 4, true}} {
		b := halfake.New(halfake.Config{Channels: c.chans})
		inst := instrument.New(instrument.State{}, nil)
		q := &countQuiet{}
		fr := scopeweb.New(scopeweb.Config{Board: b, Instrument: inst, Frames: fanout.NewFrames(), Quiet: q}).Frames()
		if _, v2 := fr.(interface{ V2Only() }); v2 != c.v2 {
			t.Errorf("%s: v2 only %v", c.name, v2)
		}
		qf, ok := fr.(webserver.Quiet)
		if !ok {
			t.Fatalf("%s: the frames take no quiet lock", c.name)
		}
		qf.QuietRLock()
		qf.QuietRUnlock()
		if q.taken != 1 || q.held != 0 {
			t.Errorf("%s: taken %d held %d", c.name, q.taken, q.held)
		}
		if _, ok := scopeweb.New(scopeweb.Config{Board: b, Instrument: inst, Frames: fanout.NewFrames()}).Frames().(webserver.Quiet); ok {
			t.Errorf("%s: frames without a quiet lock take one", c.name)
		}
	}
}
