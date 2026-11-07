// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB

package scopeweb_test

import (
	"testing"

	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// The caps' identity names the firmware (product: the page's title and its
// exports' headers) and its version beside the unit's model.
// TRLC-LINKS: REQ-DSO-164
func TestCapsIdentityNamesTheFirmware(t *testing.T) {
	r := newRig(t, halfake.Config{Channels: 4})
	s, ok := r.web.(interface{ CapsDocument() wire.Caps })
	if !ok {
		t.Fatalf("%T serves no caps document", r.web)
	}
	id := s.CapsDocument().Identity
	if id.Product != "open-dso test" || id.Firmware != "test" || id.Model != r.b.Profile().Identity.Model {
		t.Fatalf("identity %+v", id)
	}
}
