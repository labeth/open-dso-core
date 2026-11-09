// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP

package scopeapp

import (
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
)

// quietBoard is the fake on a core it shares with the display.
type quietBoard struct{ *halfake.Board }

func (b quietBoard) Profile() hal.Profile {
	p := b.Board.Profile()
	p.Budget.QuietAcquire = true
	return p
}

// pacedQuietBoard paces its records itself, on a shared core all the same.
type pacedQuietBoard struct{ quietBoard }

func (pacedQuietBoard) SetDisplayPace(time.Duration) {}

// owner is an owner loop's pace and quiet lock.
type owner struct{}

func (owner) SetDisplayPace(time.Duration) {}
func (owner) QuietRLock()                  {}
func (owner) QuietRUnlock()                {}

// A picture and a web reply take the owner's quiet lock only on a board
// that shares its core with the display and does not pace itself.
// TRLC-LINKS: REQ-DSO-168, REQ-DSO-163
func TestQuietOf(t *testing.T) {
	fake := halfake.New(halfake.Config{})
	for _, c := range []struct {
		name  string
		b     hal.Board
		owner QuietPacer
		want  bool
	}{
		{"shared core", quietBoard{fake}, owner{}, true},
		{"own core", fake, owner{}, false},
		{"no owner yet", quietBoard{fake}, nil, false},
		{"paces itself", pacedQuietBoard{quietBoard{fake}}, owner{}, false},
	} {
		if got := quietOf(c.b, c.owner) != nil; got != c.want {
			t.Errorf("%s: quiet %v, want %v", c.name, got, c.want)
		}
		if got := boardEngineOf(c.b, c.owner).quiet != nil; got != c.want {
			t.Errorf("%s: the display's quiet %v, want %v", c.name, got, c.want)
		}
	}
}
