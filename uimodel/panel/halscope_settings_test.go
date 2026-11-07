package panel

import (
	"bytes"
	"math"
	"strconv"
	"sync"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/settings"
)

// memKV is a store in memory; with slots it keeps only the keys 1 … slots,
// as a board's numbered setup store does.
type memKV struct {
	mu    sync.Mutex
	m     map[string][]byte
	slots int
}

func (kv *memKV) Get(k string) ([]byte, bool) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	b, ok := kv.m[k]
	return b, ok
}

func (kv *memKV) Put(k string, v []byte) error {
	if kv.slots > 0 {
		if n, err := strconv.Atoi(k); err != nil || n < 1 || n > kv.slots {
			return hal.Refuse("setup slot "+k, "the slots are numbered")
		}
	}
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if kv.m == nil {
		kv.m = map[string][]byte{}
	}
	kv.m[k] = append([]byte(nil), v...)
	return nil
}

func (*memKV) Budget() int { return 0 }

// A HAL scope keeps its setup through the core's settings: every channel
// of the Model (C3 and C4 in ch_more), the trigger level in volts and the
// UI model's slice, collected, stored, loaded and applied to a fresh scope
// that comes back the same.
// TRLC-LINKS: REQ-DSO-089, REQ-DSO-072
func TestHALScopeSettingsRoundTrip(t *testing.T) {
	c, m, b := halRig(t, 4)
	s := NewHALScope(m, b, "")
	ext := settings.ModelLevel{Model: m}
	_ = m.Change(func(st *instrument.State) error {
		st.TimeDivS = 1e-6
		st.Channels[2].VoltsDiv, st.Channels[2].OffsetV, st.Channels[2].Probe = 2, 0.5, 10
		st.Channels[3].Coupling, st.Channels[3].BWLimit = hal.AC, 20e6
		st.Trigger.LevelV, st.Trigger.Source.Index = -0.25, 2
		return nil
	})
	c.mu.Lock()
	c.viewMode = 2
	c.mu.Unlock()
	want := settings.Collect(m, s.Settings(), c, ext)
	if want.ChMore[0].VdivV != 0.2 || want.ChMore[0].Probe != 10 || want.ChMore[1].Coupling != 1 || !want.ChMore[1].BWL ||
		!want.Trigger.LevelSet || want.Trigger.LevelV != -0.25 || want.Trigger.Source != 2 || want.ViewMode != 2 {
		t.Fatalf("collected %+v", want)
	}

	kv := &memKV{}
	if err := settings.Save(kv, settings.FileKey, want); err != nil {
		t.Fatal(err)
	}
	raw, _ := kv.Get(settings.FileKey)
	for _, key := range [][]byte{[]byte(`"ch_more"`), []byte(`"level_v": -0.25`), []byte(`"level_set": true`)} {
		if !bytes.Contains(raw, key) {
			t.Fatalf("the file has no %s:\n%s", key, raw)
		}
	}
	got, ok := settings.Load(kv, settings.FileKey, t.Logf)
	if !ok || got != want {
		t.Fatalf("loaded %v %+v", ok, got)
	}

	c2, m2, b2 := halRig(t, 4)
	settings.Apply(got, m2, NewHALScope(m2, b2, "").Settings(), c2, settings.ModelLevel{Model: m2}, t.Logf)
	st := m2.State()
	if ch := st.Channels[2]; ch.VoltsDiv != 2 || ch.Probe != 10 || math.Abs(ch.OffsetV-0.5) > 1e-12 {
		t.Fatalf("C3 restored as %+v", ch)
	}
	if ch := st.Channels[3]; ch.Coupling != hal.AC || ch.BWLimit == 0 {
		t.Fatalf("C4 restored as %+v", ch)
	}
	if st.Trigger.LevelV != -0.25 || st.Trigger.Source.Index != 2 || st.TimeDivS != 1e-6 {
		t.Fatalf("trigger and timebase restored as %+v, %g", st.Trigger, st.TimeDivS)
	}
	if again := settings.Collect(m2, NewHALScope(m2, b2, "").Settings(), c2, settings.ModelLevel{Model: m2}); again != want {
		t.Fatalf("restored scope collects\n%+v\nwant\n%+v", again, want)
	}

	// The board's numbered setup store: slot n under "n", a named file
	// refused with why.
	slots := settings.Slots{KV: &memKV{slots: 20}, Collect: func() settings.Settings { return want }, Apply: func(settings.Settings) {}}
	if err := slots.SaveSetup(3); err != nil {
		t.Fatal(err)
	}
	if _, ok := slots.PeekSetup(3); !ok {
		t.Fatal("slot 3 is empty after a save")
	}
	if err := slots.SaveSetupFile("bench.json"); err == nil {
		t.Fatal("a numbered store took a named setup")
	}
}

// A setup with no level saved (an SDS1000CML+ file: level_code only)
// leaves a HAL scope's level as it is.
// TRLC-LINKS: REQ-DSO-072
func TestModelLevelKeepsAnUnsavedLevel(t *testing.T) {
	_, m, _ := halRig(t, 2)
	_ = m.Change(func(st *instrument.State) error { st.Trigger.LevelV = 0.7; return nil })
	settings.ModelLevel{Model: m}.ApplyTrigger(settings.Trigger{LevelCode: 30500})
	if got := m.State().Trigger.LevelV; got != 0.7 {
		t.Fatalf("level %g, want 0.7 kept", got)
	}
}
