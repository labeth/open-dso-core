package engine

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
)

// imageBoard is the fake with two images: the general one and a qualifier
// image whose trigger takes zones; its converters are checked when the
// test says so.
type imageBoard struct {
	*halfake.Board
	im *fakeImages
}

type fakeImages struct {
	mu       sync.Mutex
	current  string
	loads    []string
	fail     string // a feature whose load fails
	zones    [][]hal.Zone
	due      bool
	checks   int
	reloaded bool
}

func newImageBoard() imageBoard {
	return imageBoard{Board: halfake.New(halfake.Config{}), im: &fakeImages{current: "general"}}
}

func (b imageBoard) Images() (hal.Images, bool) { return b.im, true }
func (b imageBoard) Trigger() hal.Trigger       { return zoneTrigger{b.Board.Trigger(), b.im} }
func (b imageBoard) Platform() hal.Platform     { return checkedPlatform{b.Board.Platform(), b.im} }

func (im *fakeImages) Provides() []hal.ImageSet {
	return []hal.ImageSet{{Name: "general", Features: []string{"general"}}, {Name: "qualifier", Features: []string{"qualify"}}}
}

func (im *fakeImages) Current() hal.ImageSet {
	im.mu.Lock()
	defer im.mu.Unlock()
	return hal.ImageSet{Name: im.current, Features: []string{im.current}}
}

func (im *fakeImages) Load(_ context.Context, f []string) error {
	im.mu.Lock()
	defer im.mu.Unlock()
	want := "general"
	if len(f) > 0 {
		want = f[0]
	}
	im.loads = append(im.loads, want)
	if want == im.fail {
		return errors.New("the load failed")
	}
	im.current = want
	return nil
}

func (im *fakeImages) Choose(t hal.TriggerSpec, keep bool) []string {
	im.mu.Lock()
	defer im.mu.Unlock()
	if len(t.Stages) > 0 && len(t.Stages[0].Zones) > 0 || keep && im.current == "qualify" {
		return []string{"qualify"}
	}
	return []string{"general"}
}

func (im *fakeImages) loaded() []string {
	im.mu.Lock()
	defer im.mu.Unlock()
	return slices.Clone(im.loads)
}

type zoneTrigger struct {
	hal.Trigger
	im *fakeImages
}

func (t zoneTrigger) Program(s hal.TriggerSpec) error {
	if len(s.Stages) == 1 && len(s.Stages[0].Zones) > 0 {
		t.im.mu.Lock()
		on := t.im.current == "qualify"
		if on {
			t.im.zones = append(t.im.zones, slices.Clone(s.Stages[0].Zones))
		}
		t.im.mu.Unlock()
		if !on {
			return hal.Refuse("zone trigger", "this image has no zone trigger")
		}
		s.Stages = []hal.TrigStage{s.Stages[0]}
		s.Stages[0].Zones = nil
	}
	return t.Trigger.Program(s)
}

type checkedPlatform struct {
	hal.Platform
	im *fakeImages
}

func (p checkedPlatform) CheckDue() bool {
	p.im.mu.Lock()
	defer p.im.mu.Unlock()
	return p.im.due
}

func (p checkedPlatform) CheckConverters(context.Context) bool {
	p.im.mu.Lock()
	defer p.im.mu.Unlock()
	p.im.due = false
	p.im.checks++
	return p.im.reloaded
}

// waitUntil polls cond for up to 5 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("never: %s", what)
}

// Zones armed in the instrument state run on the image the board chooses
// for them: loaded between acquisitions, the trigger programmed with the
// zones in volts after, and the general image again once they are off.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-014
func TestHALZonesLoadTheirImage(t *testing.T) {
	b := newImageBoard()
	inst := instrument.New(instrument.State{
		Channels: []hal.Channel{{On: true, VoltsDiv: 1}, {VoltsDiv: 1}}, TimeDivS: 1e-6, Running: true, Sweep: hal.Auto,
	}, nil)
	c := startLoop(b, inst)
	defer c.stop()
	c.waitFor(t, 1)
	inst.SetZones([]instrument.Zone{{DtLoS: -1e-6, DtHiS: 1e-6, CodeLo: 103, CodeHi: 153}})
	inst.SetZoneMode(instrument.ZoneTrigger)
	waitUntil(t, "the zones on the qualifier image", func() bool { return inst.Snapshot().ZoneBackend == "hardware" })
	if got := b.im.loaded(); !slices.Equal(got, []string{"qualify"}) {
		t.Fatalf("loads %v", got)
	}
	b.im.mu.Lock()
	z := b.im.zones[len(b.im.zones)-1]
	b.im.mu.Unlock()
	if len(z) != 1 || z[0].Low != -1 || z[0].High != 1 || z[0].FromS != -1e-6 {
		t.Fatalf("zones %+v", z)
	}
	if st := inst.Snapshot(); st.Image != "qualify" || st.ZoneMode != 1 || st.ZoneCount != 1 {
		t.Fatalf("stats %+v", st)
	}
	inst.SetZoneMode(instrument.ZoneOff)
	waitUntil(t, "back on the general image", func() bool { return b.im.Current().Name == "general" })
	if st := inst.Snapshot(); st.ZoneBackend != "" {
		t.Fatalf("zone backend %q with the zones off", st.ZoneBackend)
	}
}

// A load that fails goes back to the base image and is not tried again
// until the choice changes: the zones say what the base image's trigger
// says of them, and the trigger runs without them.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-014, REQ-DSO-223
func TestHALImageLoadFailsOnce(t *testing.T) {
	b := newImageBoard()
	b.im.fail = "qualify"
	inst := instrument.New(instrument.State{
		Channels: []hal.Channel{{On: true, VoltsDiv: 1}, {VoltsDiv: 1}}, TimeDivS: 1e-6, Running: true, Sweep: hal.Auto,
		ZoneMode: instrument.ZoneTrigger, Zones: []instrument.Zone{{DtHiS: 1e-6, CodeLo: 100, CodeHi: 150}},
	}, nil)
	c := startLoop(b, inst)
	defer c.stop()
	c.waitFor(t, 3)
	if got := b.im.loaded(); !slices.Equal(got, []string{"qualify", "general"}) {
		t.Fatalf("loads %v", got)
	}
	if be := inst.Snapshot().ZoneBackend; !strings.Contains(be, "no zone trigger") {
		t.Fatalf("zone backend %q", be)
	}
}

// The board's periodic converter check runs between acquisitions while the
// run captures; a recovery that loaded the image again commits the state
// again.
// TRLC-LINKS: REQ-DSO-231
func TestHALConverterCheck(t *testing.T) {
	b := newImageBoard()
	inst := instrument.New(instrument.State{
		Channels: []hal.Channel{{On: true, VoltsDiv: 1}, {VoltsDiv: 1}}, TimeDivS: 1e-6, Running: true, Sweep: hal.Auto,
	}, nil)
	c := startLoop(b, inst)
	defer c.stop()
	c.waitFor(t, 1)
	b.im.mu.Lock()
	b.im.due, b.im.reloaded = true, true
	b.im.mu.Unlock()
	waitUntil(t, "a check", func() bool {
		b.im.mu.Lock()
		defer b.im.mu.Unlock()
		return b.im.checks == 1
	})
	n := len(c.waitFor(t, 1))
	c.waitFor(t, n+2) // the captures go on after the recovery
}

// Equivalent-time sampling is what the plan says: a board that samples in
// real time snaps it off.
// TRLC-LINKS: REQ-DSO-019, REQ-DSO-010
func TestHALETSIsThePlans(t *testing.T) {
	b := newImageBoard()
	inst := instrument.New(instrument.State{
		Channels: []hal.Channel{{On: true, VoltsDiv: 1}, {VoltsDiv: 1}}, TimeDivS: 1e-6, Running: true, Sweep: hal.Auto,
	}, nil)
	c := startLoop(b, inst)
	defer c.stop()
	c.waitFor(t, 1)
	inst.SetETS(true)
	waitUntil(t, "ETS snapped off", func() bool { _, g := inst.Wanted(); return !inst.State().ETS && g > 1 })
}
