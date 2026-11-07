// Package watchdog keeps a board's hardware watchdog after the takeover.
// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-WATCHDOG
package watchdog

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
)

// Keeper holds a board's hardware watchdog after the takeover. Once the
// vendor app is gone nothing else services it, and an unserviced watchdog
// resets the SoC, which on some scopes drops the USB path until a physical
// power cycle. It is unarmed until Acquire, petted on a fixed interval while
// armed, and disarmed (the driver told to stop counting) only on a clean
// supervisor stop: the respawned supervisor re-acquires it.
// TRLC-LINKS: REQ-DSO-026, REQ-DSO-086
type Keeper struct {
	wd hal.Watchdog // nil on a board without one

	mu      sync.Mutex
	armed   bool
	stop    chan struct{}
	lastPet time.Time
	petErr  error
}

// ErrNone is Acquire on a board without a watchdog.
var ErrNone = errors.New("watchdog: this board has none")

// NewKeeper is an unarmed keeper of wd; nil is a board without a watchdog.
// TRLC-LINKS: REQ-DSO-086
func NewKeeper(wd hal.Watchdog) *Keeper { return &Keeper{wd: wd} }

// Acquire opens the watchdog, retrying until timeout (the vendor app's
// descriptor needs a moment to drain after the kill), pets once so the
// countdown is fresh, and starts the pet loop.
// TRLC-LINKS: REQ-DSO-086
func (k *Keeper) Acquire(timeout, petEvery time.Duration) error {
	if k.wd == nil {
		return ErrNone
	}
	k.mu.Lock()
	if k.armed {
		k.mu.Unlock()
		return nil
	}
	k.mu.Unlock()

	deadline := time.Now().Add(timeout)
	for {
		k.mu.Lock()
		if k.armed {
			// Another Acquire armed it while this one waited: one hold and
			// one pet loop, or Disarm stops only one.
			k.mu.Unlock()
			return nil
		}
		err := k.wd.Open()
		if err == nil {
			k.armed = true
			k.stop = make(chan struct{})
			k.petLocked() // fresh countdown immediately on acquire
			stop := k.stop
			k.mu.Unlock()
			go k.loop(petEvery, stop)
			return nil
		}
		k.mu.Unlock()
		if time.Now().After(deadline) {
			return fmt.Errorf("watchdog: %w", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TRLC-LINKS: REQ-DSO-026, REQ-DSO-086
func (k *Keeper) loop(every time.Duration, stop chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			k.mu.Lock()
			if k.armed {
				k.petLocked()
			}
			k.mu.Unlock()
		case <-stop:
			return
		}
	}
}

// TRLC-LINKS: REQ-DSO-026, REQ-DSO-086
func (k *Keeper) petLocked() {
	k.petErr = k.wd.Pet()
	k.lastPet = time.Now()
}

// Disarm stops the pet loop and closes the watchdog so the driver disarms
// instead of resetting (no-op when not armed).
// TRLC-LINKS: REQ-DSO-086
func (k *Keeper) Disarm() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if !k.armed {
		return
	}
	close(k.stop)
	_ = k.wd.Close() // the hold is given up either way; a failed close leaves the driver counting, as a crash would
	k.armed = false
}

// Status is the watchdog's state for the status and heartbeat.
// TRLC-LINKS: REQ-DSO-086
type Status struct {
	Armed   bool      `json:"armed"`
	LastPet time.Time `json:"last_pet,omitzero"`
	PetErr  string    `json:"pet_err,omitempty"`
}

// Status reports whether the watchdog is armed, its last pet and the last
// pet's failure.
// TRLC-LINKS: REQ-DSO-086
func (k *Keeper) Status() Status {
	k.mu.Lock()
	defer k.mu.Unlock()
	s := Status{Armed: k.armed, LastPet: k.lastPet}
	if k.petErr != nil {
		s.PetErr = k.petErr.Error()
	}
	return s
}
