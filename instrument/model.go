// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"context"
	"slices"
	"sync"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

// Model is the wanted instrument state and the owner's view of it.
// Controllers (SCPI handlers, the web server, the UI model) run on their
// own goroutines and never touch hardware: they edit the wanted state, ask
// for a forced trigger or post LONG work. The owner loop takes what is
// pending at its commit point, commits it through the HAL and reports back
// what the board applied, what it refused, the run state and every frame
// it publishes. Every method is safe for concurrent use.
type Model struct {
	mu      sync.Mutex
	want    State
	gen     uint64 // bumped by every change of want
	check   func(State) error
	run     RunState
	force   bool
	posted  []func(ctx context.Context)
	refused []func(error)
	frames  FrameSource
	last    *sample.Frame
	runMem  runMemory // the trigger mode an armed single shot replaced
	tdivs   []float64 // the timebase ladder SetTdiv snaps to
}

// FrameSource gives a reader the newest published frame: the engine's
// fan-out. Without one the model keeps the owner's last frame itself.
type FrameSource interface {
	WithFrame(fn func(*sample.Frame))
}

// New starts from state s. check validates a change on the caller's
// goroutine with what the board can tell without hardware (a ladder, a
// probe factor); nil checks nothing.
// TRLC-LINKS: REQ-DSO-008
func New(s State, check func(State) error) *Model {
	s = s.Clone()
	if s.Sweep == hal.Single { // a shot armed from the start
		s.Sweep, s.Single = hal.NormalSweep, true
	}
	return &Model{want: s, gen: 1, check: check, run: Armed}
}

// SetFrames makes src the frames WithFrame reads (the engine's fan-out).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) SetFrames(src FrameSource) {
	m.mu.Lock()
	m.frames = src
	m.mu.Unlock()
}

// State is the wanted state: what was last set, with the values the board
// snapped once it applied them.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.want.Clone()
}

// Change edits a copy of the wanted state (its Channels are the copy's own)
// and stages it; an error from fn or from the check leaves the state as it
// was. A change that alters nothing moves no generation. The edit keeps the
// run control's rules (runRules): a controller that writes Sweep Single
// arms a shot.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Change(fn func(*State) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, r := m.want.Clone(), m.runMem
	if err := fn(&s); err != nil {
		return err
	}
	runRules(m.want, &s, &r)
	return m.commitLocked(s, r)
}

// commitLocked makes s and the run memory r the wanted state once the
// check passes; m.mu is held.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) commitLocked(s State, r runMemory) error {
	if m.check != nil {
		if err := m.check(s); err != nil {
			return err
		}
	}
	m.runMem = r
	if !s.Equal(m.want) {
		m.want = s
		m.gen++
	}
	return nil
}

// Generation moves with every change of the wanted state.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen
}

// WithFrame calls fn with the newest published frame, nil when there is
// none. The frame is read-only and valid only inside fn.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) WithFrame(fn func(*sample.Frame)) {
	m.mu.Lock()
	src, f := m.frames, m.last
	m.mu.Unlock()
	if src != nil {
		src.WithFrame(fn)
		return
	}
	fn(f)
}

// Acquisition is the run state the owner last reported; Stopped whenever
// the wanted state is not running.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Acquisition() RunState {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.want.Running {
		return Stopped
	}
	return m.run
}

// Force asks the owner for one forced trigger.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Force() error {
	m.mu.Lock()
	m.force = true
	m.mu.Unlock()
	return nil
}

// Post queues LONG work for the owner (a self-calibration, an image
// load): it runs between acquisitions, with the acquisition stopped, and
// the next arm commits the whole state again.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Post(work func(ctx context.Context)) {
	m.mu.Lock()
	m.posted = append(m.posted, work)
	m.mu.Unlock()
}

// OnRefused adds a listener for the refusals the board answers a commit
// with: a controller whose change was accepted when staged learns here
// that the hardware did not take it (SCPI queues it as an error).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) OnRefused(fn func(error)) {
	m.mu.Lock()
	m.refused = append(m.refused, fn)
	m.mu.Unlock()
}

// Wanted is the wanted state with its generation (OWNER).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Wanted() (State, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.want.Clone(), m.gen
}

// TakeWork takes the posted LONG work (OWNER).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) TakeWork() []func(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.posted
	m.posted = nil
	return w
}

// HasWork reports posted LONG work waiting for the owner.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) HasWork() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.posted) > 0
}

// TakeForce takes a pending forced trigger (OWNER); ForcePending looks at
// it.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) TakeForce() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.force
	m.force = false
	return f
}

// ForcePending reports a forced trigger not yet taken.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) ForcePending() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.force
}

// Applied takes back the state the owner committed: the values the board
// snapped, or what the owner changed itself (a single shot that completed
// stops the run). A controller's change since generation gen wins (OWNER).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Applied(s State, gen uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen == gen {
		m.want = s.Clone()
	}
}

// Refused reports a commit the board refused to every listener (OWNER).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Refused(err error) {
	m.mu.Lock()
	ls := slices.Clone(m.refused)
	m.mu.Unlock()
	for _, fn := range ls {
		fn(err)
	}
}

// SetRun records the acquisition's run state (OWNER).
// TRLC-LINKS: REQ-DSO-008
func (m *Model) SetRun(r RunState) {
	m.mu.Lock()
	m.run = r
	m.mu.Unlock()
}

// Published records the newest frame for a model without a fan-out
// (OWNER). The frame must not change afterwards.
// TRLC-LINKS: REQ-DSO-008
func (m *Model) Published(f *sample.Frame) {
	m.mu.Lock()
	m.last = f
	m.mu.Unlock()
}
