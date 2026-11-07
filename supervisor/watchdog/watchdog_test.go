// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-WATCHDOG

package watchdog

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fileDev is a hal.Watchdog over a plain temp file: no test arms the host's
// real watchdog. It counts its open holds.
// TRLC-LINKS: REQ-DSO-086
type fileDev struct {
	path string

	mu    sync.Mutex
	f     *os.File
	opens int
}

// TRLC-LINKS: REQ-DSO-086
func (d *fileDev) Open() error {
	f, err := os.OpenFile(d.path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	d.mu.Lock()
	d.f = f
	d.opens++
	d.mu.Unlock()
	return nil
}

// TRLC-LINKS: REQ-DSO-086
func (d *fileDev) Pet() error {
	_, err := d.f.Write([]byte{0x01})
	return err
}

// TRLC-LINKS: REQ-DSO-086
func (d *fileDev) Close() error {
	d.mu.Lock()
	d.opens--
	d.mu.Unlock()
	_, _ = d.f.Write([]byte{'V'})
	return d.f.Close()
}

// TRLC-LINKS: REQ-DSO-086
func newDev(t *testing.T) *fileDev {
	t.Helper()
	p := filepath.Join(t.TempDir(), "watchdog")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return &fileDev{path: p}
}

// TRLC-LINKS: REQ-DSO-026, REQ-DSO-086
func TestAcquirePetsImmediatelyAndDisarmsWithTheMagicByte(t *testing.T) {
	d := newDev(t)
	k := NewKeeper(d)
	if err := k.Acquire(time.Second, time.Hour); err != nil {
		t.Fatal(err)
	}
	st := k.Status()
	if !st.Armed || st.LastPet.IsZero() || st.PetErr != "" {
		t.Fatalf("after acquire: %+v", st)
	}
	k.Disarm()
	b, _ := os.ReadFile(d.path)
	if string(b) != "\x01V" {
		t.Errorf("device content = %q, want a pet then the disarm byte", b)
	}
	if k.Status().Armed {
		t.Error("armed after disarm")
	}
}

// TRLC-LINKS: REQ-DSO-026, REQ-DSO-086
func TestPetLoopFeedsOnIntervalAndStopsAfterDisarm(t *testing.T) {
	d := newDev(t)
	k := NewKeeper(d)
	if err := k.Acquire(time.Second, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	first := k.Status().LastPet
	deadline := time.Now().Add(5 * time.Second)
	for pets := 0; pets < 3; {
		if time.Now().After(deadline) {
			t.Fatalf("only %d ticker pets in 5s at a 10ms interval", pets)
		}
		if last := k.Status().LastPet; last.After(first) {
			first = last
			pets++
		}
		time.Sleep(2 * time.Millisecond)
	}
	k.Disarm()
	fi1, _ := os.Stat(d.path)
	time.Sleep(100 * time.Millisecond)
	fi2, _ := os.Stat(d.path)
	if fi1.Size() != fi2.Size() {
		t.Errorf("device grew after disarm: the pet loop still runs")
	}
}

// TRLC-LINKS: REQ-DSO-086
func TestAcquireRetriesThenTimesOut(t *testing.T) {
	missing := &fileDev{path: filepath.Join(t.TempDir(), "nope")}
	k := NewKeeper(missing)
	err := k.Acquire(300*time.Millisecond, time.Hour)
	if err == nil || !strings.Contains(err.Error(), missing.path) || !strings.HasPrefix(err.Error(), "watchdog: ") {
		t.Fatalf("acquire of a missing device: %v", err)
	}
	late := &fileDev{path: filepath.Join(t.TempDir(), "late")}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = os.WriteFile(late.path, nil, 0o644)
	}()
	k = NewKeeper(late)
	if err := k.Acquire(3*time.Second, time.Hour); err != nil {
		t.Fatalf("acquire should retry until the device appears: %v", err)
	}
	k.Disarm()
}

// TRLC-LINKS: REQ-DSO-086
func TestAcquireIdempotentAndReacquirable(t *testing.T) {
	d := newDev(t)
	k := NewKeeper(d)
	k.Disarm() // no-op before any acquire
	for range 2 {
		if err := k.Acquire(time.Second, time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := k.Acquire(time.Second, time.Hour); err != nil {
			t.Fatal(err)
		}
		if d.opens != 1 {
			t.Fatalf("%d holds after two acquires, want 1", d.opens)
		}
		k.Disarm()
	}
}

// Acquires that race (the restart path and a takeover RPC both acquire) arm
// one hold: after Disarm none is left.
// TRLC-LINKS: REQ-DSO-086
func TestConcurrentAcquireLeavesOneHold(t *testing.T) {
	d := newDev(t)
	for round := range 100 {
		k := NewKeeper(d)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 16 {
			wg.Go(func() {
				<-start
				if err := k.Acquire(time.Second, time.Hour); err != nil {
					t.Error(err)
				}
			})
		}
		close(start)
		wg.Wait()
		k.Disarm()
		if d.opens != 0 {
			t.Fatalf("round %d: %d holds left after Disarm", round, d.opens)
		}
	}
}

// TRLC-LINKS: REQ-DSO-086
func TestNoWatchdog(t *testing.T) {
	k := NewKeeper(nil)
	if err := k.Acquire(time.Second, time.Hour); !errors.Is(err, ErrNone) {
		t.Errorf("acquire without a watchdog: %v", err)
	}
	k.Disarm()
	if k.Status().Armed {
		t.Error("armed without a watchdog")
	}
}
