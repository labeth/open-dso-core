// ENGMODEL-OWNER-UNIT: FU-DSO-ANALYSIS

package analysis

import (
	"slices"
	"testing"

	"github.com/labeth/open-dso-core/decode"
	"github.com/labeth/open-dso-core/decode/synth"
)

// TestSynthLayersDetected: every layer session the bus generators write
// (decode/synth) decodes on its bus, as the detection names the bus, into
// packets the layer detection names as that layer.
// TRLC-LINKS: REQ-DSO-241, REQ-DSO-247
func TestSynthLayersDetected(t *testing.T) {
	for _, l := range synth.Layers() {
		t.Run(l.ID, func(t *testing.T) {
			b0, err := synth.MakeLayer(l.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := synth.MakeLayer(l.ID, b0.NaturalSR)
			c1 := b.Lines[0].Codes()
			c2 := make([]uint8, len(c1))
			for i := range c2 {
				c2[i] = synth.CodeMid
			}
			if len(b.Lines) > 1 {
				c2 = b.Lines[1].Codes()
			}
			r, _ := decode.AutodetectFast(c1, c2, 1/b.NaturalSR, "hex")
			if r.Proto != l.Bus && slices.Contains(b.Truth.Alike, r.Proto) {
				t.Skipf("the bus reads as %q as well (synth.Truth.Alike): no layer to detect on it", r.Proto)
			}
			if r.Proto != l.Bus {
				t.Fatalf("named %q (%s), want the bus %q", r.Proto, r.Src, l.Bus)
			}
			ps := Packets(r, len(c1))
			if id, why := DetectLayer(r.Proto, ps); id != b.Truth.Layer {
				t.Errorf("layer %q (%s), want %q; %d packets", id, why, b.Truth.Layer, len(ps))
			}
		})
	}
}
