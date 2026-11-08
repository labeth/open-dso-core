package scopeapp

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/settings"
)

// defaults is a HAL scope's power-on state: every channel on at 1 V/div,
// 1 ms/div, running.
func defaults(chans int) instrument.State {
	st := instrument.State{TimeDivS: 1e-3, Running: true}
	for range chans {
		st.Channels = append(st.Channels, hal.Channel{On: true, VoltsDiv: 1, Probe: 1, Impedance: 1e6})
	}
	return st
}

// run runs the application on b until the returned stop is called, and
// hands back its composition once the builders have run.
func run(t *testing.T, b hal.Board) (*Env, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	envc := make(chan *Env, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, b, Options{
			Logf: t.Logf, Product: "test", Defaults: defaults,
			Started: func(env *Env) { envc <- env },
		})
	}()
	var env *Env
	select {
	case env = <-envc:
	case err := <-done:
		t.Fatalf("Run: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("the composition did not come up")
	}
	return env, func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Run did not return")
		}
	}
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("no %s", what)
}

// A HAL scope's application: the board comes up, the owner loop publishes
// frames the display draws into the board's framebuffer, a change is
// saved when the application stops, and the next start restores it, C3
// and the trigger level in volts too.
// TRLC-LINKS: REQ-DSO-295, REQ-DSO-089, REQ-DSO-293
func TestRunRestoresTheSetupAcrossARestart(t *testing.T) {
	b := halfake.New(halfake.Config{Channels: 4})
	env, stop := run(t, b)
	if env.Model == nil || env.UI == nil || env.HALScope == nil || len(env.Model.State().Channels) != 4 {
		t.Fatalf("composition %+v", env)
	}
	var seq uint64
	waitFor(t, "frame", func() bool {
		seq = env.Frames.WaitNext(0, 100*time.Millisecond)
		return seq > 0
	})
	waitFor(t, "picture on the framebuffer", func() bool { return b.Presents() > 0 })

	_ = env.Model.Change(func(st *instrument.State) error {
		st.Channels[2].VoltsDiv = 2
		return st.Trigger.SetLevel(0.25, len(st.Channels))
	})
	stop()
	raw, ok := b.Storage().Settings().Get(settings.FileKey)
	if !ok || !bytes.Contains(raw, []byte(`"ch_more"`)) || !bytes.Contains(raw, []byte(`"level_v": 0.25`)) {
		t.Fatalf("the stop saved no setup:\n%s", raw)
	}

	env, stop = run(t, b)
	defer stop()
	waitFor(t, "restored setup", func() bool {
		st := env.Model.State()
		return st.Channels[2].VoltsDiv == 2 && st.Trigger.LevelV() == 0.25
	})
	if env.Setups == nil {
		t.Fatal("no setup memory")
	}
}

// copyRows lays a picture's rows into a wider framebuffer, the rows asked
// for only.
// TRLC-LINKS: REQ-DSO-168
func TestCopyRows(t *testing.T) {
	src := []byte{1, 2, 3, 4, 5, 6}
	dst := make([]byte, 12)
	copyRows(dst, 4, src, 2, 0, 3)
	if string(dst) != string([]byte{1, 2, 0, 0, 3, 4, 0, 0, 5, 6, 0, 0}) {
		t.Fatalf("%v", dst)
	}
	dst = make([]byte, 12)
	copyRows(dst, 4, src, 2, 1, 2)
	if string(dst) != string([]byte{0, 0, 0, 0, 3, 4, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("row 1 alone: %v", dst)
	}
}
