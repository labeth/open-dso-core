// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS

package settings

import (
	"testing"
	"time"
)

// A poll with nothing changed: the saver's steady state, once a second.
// TRLC-LINKS: REQ-DSO-293
func BenchmarkSaverStepSteady(b *testing.B) {
	cur := Settings{Version: Version, TdivS: 1e-3}
	s := NewSaver(FileKV{}, FileKey, func() Settings { return cur }, nil)
	s.save = func(Settings) error { return nil }
	s.step()
	for b.Loop() {
		s.step()
	}
}

// TRLC-LINKS: REQ-DSO-072
func BenchmarkSlotPath(b *testing.B) {
	n := 0
	for b.Loop() {
		_ = SlotFile(n % 20)
		n++
	}
}

// The saver still saves a change once it settles, and a change back to the
// saved state needs no save.
// TRLC-LINKS: REQ-DSO-293
func TestSaverSavesOnlyASettledChange(t *testing.T) {
	cur := Settings{Version: Version, TdivS: 1e-3}
	now := time.Unix(0, 0)
	saves := 0
	s := NewSaver(FileKV{}, FileKey, func() Settings { return cur }, nil)
	s.save = func(Settings) error { saves++; return nil }
	s.now = func() time.Time { return now }
	s.step()
	cur.TdivS = 2e-3
	for range 5 {
		now = now.Add(time.Second)
		s.step()
	}
	if saves != 1 {
		t.Fatalf("%d saves after a settled change, want 1", saves)
	}
	cur.TdivS = 5e-3
	now = now.Add(time.Second)
	s.step()
	cur.TdivS = 2e-3
	for range 5 {
		now = now.Add(time.Second)
		s.step()
	}
	if saves != 1 {
		t.Fatalf("%d saves after changing back to the saved setup, want 1", saves)
	}
}

// The SDS1000CML+'s slot files are setup00.json to setup99.json.
// TRLC-LINKS: REQ-DSO-072
func TestSlotFileNames(t *testing.T) {
	for n, want := range map[int]string{0: "setup00.json", 7: "setup07.json", 19: "setup19.json", 123: "setup123.json"} {
		if got := SlotFile(n); got != want {
			t.Fatalf("slot %d: %s, want %s", n, got, want)
		}
	}
}
