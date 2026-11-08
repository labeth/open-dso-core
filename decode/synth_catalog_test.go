// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package decode

import (
	"slices"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/decode/synth"
)

// synthCodes is a bus's lines as the two channels the detection reads: a
// one-line bus on C1 beside an idle C2, a two-line bus's lines on C1 and C2.
func synthCodes(b synth.Bus) (c1, c2 []uint8) {
	c1 = b.Lines[0].Codes()
	if len(b.Lines) > 1 {
		return c1, b.Lines[1].Codes()
	}
	return c1, idleLike(c1)
}

// TestSynthBusesDecode: every generator's bus, in each of its variants, is
// named by the detection the scope runs (AutodetectFast) as its protocol
// (or a name its Truth allows) and decodes to its ground truth: the bytes
// it wrote, as the decoder reports them, and the tokens and messages it
// must show.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-238
func TestSynthBusesDecode(t *testing.T) {
	ps := synth.Protocols()
	if len(ps) != 52 {
		t.Errorf("%d protocols, want the decoders' 52", len(ps))
	}
	for _, p := range ps {
		for _, s := range synth.Variants(p.Proto) {
			t.Run(s.String(), func(t *testing.T) {
				b, err := synth.Make(s, 0)
				if err != nil {
					t.Fatal(err)
				}
				if b, err = synth.Make(s, b.NaturalSR); err != nil {
					t.Fatal(err)
				}
				c1, c2 := synthCodes(b)
				r, _ := AutodetectFast(c1, c2, 1/b.NaturalSR, "hex")
				if r.Proto != b.Proto && !slices.Contains(b.Truth.Alike, r.Proto) {
					t.Errorf("%s: named %q (%s), want %q", b.Name, r.Proto, r.Src, b.Proto)
					return
				}
				if r.Proto != b.Proto {
					return
				}
				if b.Truth.Bytes != nil && !slices.Equal(r.Bytes, b.Truth.Bytes) {
					t.Errorf("%s: bytes % x, want % x (%s)", b.Name, r.Bytes, b.Truth.Bytes, r.Text)
				}
				for _, w := range slices.Concat(b.Truth.Contains, b.Truth.Messages) {
					if !strings.Contains(r.Text, w) {
						t.Errorf("%s: text %q lacks %q", b.Name, r.Text, w)
					}
				}
			})
		}
	}
}
