// ENGMODEL-OWNER-UNIT: FU-DSO-WEBCLIENT

package webclient

import (
	"os/exec"
	"testing"

	"github.com/labeth/open-dso-core/testenv"
)

// TestCapsNJS runs caps_n.test.cjs: SOUND's lanes, BUS Visual's records,
// references, super-res stacks, FFT peak lists and the trigger level over a
// four-channel scope.
// TRLC-LINKS: REQ-DSO-204
func TestCapsNJS(t *testing.T) {
	testenv.NeedNode(t)
	out, err := exec.CommandContext(t.Context(), "node", "caps_n.test.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("caps_n.test.cjs: %v\n%s", err, out)
	}
}
