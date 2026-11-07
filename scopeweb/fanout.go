// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb

import (
	"github.com/labeth/open-dso-core/engine/fanout"
	"github.com/labeth/open-dso-core/sample"
)

// NewFanout is the engine's frame fan-out for a cycle that does not number
// the frames it publishes (the HAL cycle publishes them with seq 0): each
// snapshot of a frame without a seq takes the next one, so a page can ask
// for the frame after the one it holds. A frame with its own seq keeps it.
// TRLC-LINKS: REQ-DSO-163
func NewFanout() *fanout.Fanout[sample.Frame] {
	var next uint64
	return fanout.New(func(dst, src *sample.Frame) {
		dst.CopyFrom(src)
		if dst.Seq == 0 {
			next++
			dst.Seq = next
		}
	}, func(f *sample.Frame) uint64 { return f.Seq })
}
