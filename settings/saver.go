// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS

package settings

import (
	"sync"
	"time"

	"github.com/labeth/open-dso-core/hal"
)

// Saver watches the live setup and persists it, debounced. It is the one
// observation point for every mutation path: panel knobs and menus, the
// web's settings, injected panel actions and SCPI all end in the same
// state (the instrument's controls, the front end and the UI model), so
// polling that state catches them all without save hooks in the setters.
//
// Policy: poll once a second (cheap snapshot copies, never on the acquisition
// hot path), and write only after the setup has been STABLE for the settle
// window (default 2 s) and actually differs from what is on disk. A knob
// sweep therefore costs one write, ~2–3 s after the last detent.
// TRLC-LINKS: REQ-DSO-293
type Saver struct {
	name   string // where the setup goes, for the log
	snap   func() Settings
	logf   func(string, ...any)
	poll   time.Duration
	settle time.Duration

	now  func() time.Time      // injectable clock (tests)
	save func(Settings) error // injectable sink (tests)

	mu         sync.Mutex
	primed     bool
	last       Settings  // latest observed state
	lastChange time.Time // when `last` last changed
	saved      Settings  // what we believe is on disk
	unsaved    bool      // last != saved: kept so a still poll compares once
	fails      int       // consecutive save failures (log throttle)
}

// NewSaver builds a saver of the setup under key in kv; snap assembles the
// current setup (typically a Collect closure). Call Run on a goroutine
// after the restore has been applied: the first poll primes the "stored"
// shadow from the live state, so an unchanged setup is never written again.
// TRLC-LINKS: REQ-DSO-293
func NewSaver(kv hal.KV, key string, snap func() Settings, logf func(string, ...any)) *Saver {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Saver{
		name: nameOf(kv, key), snap: snap, logf: logf,
		poll: time.Second, settle: 2 * time.Second,
		now: time.Now, save: func(s Settings) error { return Save(kv, key, s) },
	}
}

// Run polls until stop closes, then flushes any pending change.
// TRLC-LINKS: REQ-DSO-293
func (s *Saver) Run(stop <-chan struct{}) {
	t := time.NewTicker(s.poll)
	defer t.Stop()
	for {
		select {
		case <-stop:
			s.Flush()
			return
		case <-t.C:
			s.step()
		}
	}
}

// step is one poll: observe, debounce, maybe save.
// TRLC-LINKS: REQ-DSO-293
func (s *Saver) step() {
	cur := s.snap()
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.primed {
		// Baseline: the state as of saver start IS the restored state, so
		// nothing is dirty yet. (If the restore was partial — e.g. a ladder
		// value the engine rejected — the next real change resyncs the file.)
		s.last, s.saved, s.primed, s.unsaved = cur, cur, true, false
		return
	}
	if cur != s.last {
		s.last, s.lastChange = cur, now // still changing — restart the settle window
		s.unsaved = cur != s.saved
		return
	}
	if !s.unsaved || now.Sub(s.lastChange) < s.settle {
		return
	}
	s.persistLocked(cur)
}

// Flush writes immediately when the current state differs from the stored
// one: the shutdown path, so a change still inside the debounce window
// survives a supervisor's restart (SIGTERM, then a relaunch).
// TRLC-LINKS: REQ-DSO-293
func (s *Saver) Flush() {
	cur := s.snap()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.primed && cur == s.saved {
		return
	}
	s.persistLocked(cur)
	s.last, s.primed = cur, true
	s.unsaved = cur != s.saved
}

// persistLocked writes cur and updates the stored shadow. Failures retry on
// the next poll (a USB stick can flake to read-only under load); the log is
// throttled so a persistently broken store cannot flood the log.
// TRLC-LINKS: REQ-DSO-293
func (s *Saver) persistLocked(cur Settings) {
	if err := s.save(cur); err != nil {
		s.fails++
		if s.fails == 1 || s.fails%60 == 0 {
			s.logf("settings: save %s: %v (attempt %d)", s.name, err, s.fails)
		}
		return
	}
	if s.fails > 0 {
		s.logf("settings: save recovered after %d failures", s.fails)
	}
	s.fails = 0
	s.saved = cur
	s.unsaved = s.last != cur
	s.logf("settings: saved %s", s.name)
}
