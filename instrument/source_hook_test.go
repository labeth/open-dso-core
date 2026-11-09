// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// A trigger source change is heard once it is staged.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-098
func TestOnTrigSourceHearsTheSource(t *testing.T) {
	m := New(State{Channels: make([]hal.Channel, 2)}, nil)
	var heard []int
	m.OnTrigSource(func() { heard = append(heard, m.State().Trigger.Source().Index) })
	m.SetTrigSource(1)
	m.SetTrigSource(0)
	if len(heard) != 2 || heard[0] != 1 || heard[1] != 0 {
		t.Fatalf("heard %v, want [1 0]", heard)
	}
}
