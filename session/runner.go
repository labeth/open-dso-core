package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/uimodel"
)

// SignalDriver puts signals on a target's inputs.
type SignalDriver interface {
	// Can reports whether the driver can produce sigs, and why not.
	Can(sigs map[string]Signal) (bool, string)
	// Apply puts sigs on the inputs.
	Apply(ctx context.Context, sigs map[string]Signal) error
}

// BoardInput presses keys and turns knobs through a board's own panel
// queue, as a hand on the bench does.
type BoardInput interface {
	Key(ctx context.Context, name string) error
	Knob(ctx context.Context, name string, steps int) error
}

// Target is where sessions run.
type Target struct {
	Name    string
	Client  *Client
	Signals SignalDriver
	Input   BoardInput // nil: the target has no board panel queue
	// MinimalRelay skips every session that clicks front-end relays, and
	// any start or restore that would.
	MinimalRelay bool
	// Restore puts the scope's state from before each session back after
	// it, through the web API.
	Restore bool
	// Log hears progress; Plan, set on the client, makes the run a dry run.
	Log io.Writer
	// SettleMax bounds one settle (default 20 s); AUTO gets twice.
	SettleMax time.Duration
}

// Run is one target's runs of a set of sessions.
type Run struct {
	Target   string    `json:"target"`
	Sessions []*Played `json:"sessions"`
}

// Played is one session played on one target.
type Played struct {
	Session  *Session    `json:"session"`
	Skipped  string      `json:"skipped,omitempty"`
	Steps    []StepRun   `json:"steps"`
	Checks   []*CheckRun `json:"checks"`
	Outcomes []Outcome   `json:"expectations"`
	Restored string      `json:"restored,omitempty"`
}

// StepRun is one step's outcome.
type StepRun struct {
	Step string `json:"step"`
	OK   bool   `json:"ok"`
	Err  string `json:"err,omitempty"`
	NA   string `json:"na,omitempty"` // the target cannot perform the step
	Ms   int64  `json:"ms"`
}

// CheckRun is one checkpoint's snapshot. Tainted says why its fields
// cannot be compared (an earlier step the target could not perform).
type CheckRun struct {
	Name     string   `json:"name"`
	Snapshot Snapshot `json:"snapshot"`
	Tainted  string   `json:"tainted,omitempty"`
}

// dry reports whether the run only prints what it would do.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) dry() bool { return t.Client.Plan != nil }

// logf writes a progress line.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) logf(format string, a ...any) {
	if t.Log != nil {
		_, _ = fmt.Fprintf(t.Log, format+"\n", a...)
	}
}

// RunAll runs every session on t in order.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) RunAll(ctx context.Context, sessions []*Session, base Baseline) *Run {
	r := &Run{Target: t.Name}
	for _, s := range sessions {
		if ctx.Err() != nil {
			break
		}
		r.Sessions = append(r.Sessions, t.RunSession(ctx, s, base))
	}
	return r
}

// skipReason is why t cannot run s at all; "" when it can.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) skipReason(s *Session) string {
	if slices.Contains(s.Needs, NeedBoardInput) && t.Input == nil {
		return "needs the board's panel input, which this target does not offer"
	}
	if t.MinimalRelay && s.Relays {
		return "clicks front-end relays (minimal-relay run)"
	}
	if ok, why := t.Signals.Can(s.Signals); !ok {
		return why
	}
	for _, st := range s.Steps {
		if len(st.Signals) > 0 {
			if ok, why := t.Signals.Can(st.Signals); !ok {
				return why
			}
		}
	}
	return ""
}

// RunSession runs s on t: its signals, its start state, its steps, and the
// restore of the state the scope had before it.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) RunSession(ctx context.Context, s *Session, base Baseline) *Played {
	sr := &Played{Session: s}
	t.logf("== %s on %s: %s", s.Name, t.Name, s.About)
	if why := t.skipReason(s); why != "" {
		sr.Skipped = why
		t.logf("   skipped: %s", why)
		return sr
	}
	var before *savedState
	if t.Restore {
		t.logf("  record the scope's state to restore it afterwards")
		saved, err := t.save(ctx)
		if err != nil && !errors.Is(err, ErrPlanned) {
			sr.Skipped = "cannot record the state to restore: " + err.Error()
			return sr
		}
		before = saved
	}
	if len(s.Signals) > 0 {
		t.logf("  signals: %s", DescribeSignals(s.Signals))
		if err := t.Signals.Apply(ctx, s.Signals); err != nil && !errors.Is(err, ErrPlanned) {
			sr.Skipped = "signals: " + err.Error()
			return sr
		}
	}
	if err := t.start(ctx, s, base); err != nil {
		sr.Skipped = "start state: " + err.Error()
		t.restore(ctx, sr, before)
		return sr
	}
	tainted := ""
	earlier := map[string]Snapshot{}
	for _, st := range s.Steps {
		if st.Check != nil {
			cr := &CheckRun{Name: st.Check.Name, Tainted: tainted}
			t.logf("  check %s", st.Check.Name)
			snap, err := Capture(ctx, t.Client)
			switch {
			case errors.Is(err, ErrPlanned):
				snap = Snapshot{}
			case err != nil:
				snap = Snapshot{"": na("capture failed: " + err.Error())}
				cr.Tainted = "the capture failed: " + err.Error()
			}
			cr.Snapshot = snap
			sr.Checks = append(sr.Checks, cr)
			if !t.dry() {
				sr.Outcomes = append(sr.Outcomes, evaluate(st.Check, snap, earlier, cr.Tainted)...)
			}
			earlier[st.Check.Name] = snap
			continue
		}
		run := t.step(ctx, st)
		sr.Steps = append(sr.Steps, run)
		switch {
		case run.NA != "" && tainted == "":
			tainted = fmt.Sprintf("step %q cannot run on this target: %s", run.Step, run.NA)
		case !run.OK && run.NA == "" && tainted == "":
			tainted = fmt.Sprintf("step %q failed: %s", run.Step, run.Err)
		}
	}
	t.restore(ctx, sr, before)
	return sr
}

// step performs one step and settles.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) step(ctx context.Context, st *Step) StepRun {
	t0 := time.Now()
	run := StepRun{Step: st.String()}
	t.logf("  %s", run.Step)
	var err error
	settle := t.SettleMax
	if settle <= 0 {
		settle = 20 * time.Second
	}
	switch st.Kind() {
	case KindKey:
		if st.Key == "auto" {
			settle *= 2
		}
		if st.Via == ViaBoard {
			if t.Input == nil {
				run.NA = "no board panel input"
				return run
			}
			err = t.Input.Key(ctx, st.Key)
		} else {
			err = t.Client.Post(ctx, "/api/panel", map[string]any{"button": st.Key})
		}
	case KindKnob:
		if st.Via == ViaBoard {
			if t.Input == nil {
				run.NA = "no board panel input"
				return run
			}
			err = t.Input.Knob(ctx, st.Knob, st.Steps)
		} else {
			dir, n := 1, st.Steps
			if n < 0 {
				dir, n = -1, -n
			}
			err = t.Client.Post(ctx, "/api/panel", map[string]any{"knob": st.Knob, "dir": dir, "steps": n})
		}
	case KindWorkspace:
		err = t.Client.Post(ctx, "/api/panel", map[string]any{KindWorkspace: st.Workspace})
	case KindRow:
		err = t.setRow(ctx, st.Row.ID, st.Row.Text, st.Row.Value)
	case KindSet:
		body := map[string]any{"control": st.Set.Control, "value": st.Set.Value}
		if st.Set.Ch != nil {
			body["ch"] = *st.Set.Ch
		}
		err = t.Client.Post(ctx, "/api/set", body)
	case KindSignals:
		err = t.Signals.Apply(ctx, st.Signals)
	case KindWait:
		if !t.dry() {
			sleep(ctx, time.Duration(st.WaitS*float64(time.Second)))
		}
		run.OK, run.Ms = true, time.Since(t0).Milliseconds()
		return run
	}
	if errors.Is(err, ErrPlanned) {
		err = nil
	}
	if err == nil {
		err = t.settle(ctx, settle)
	}
	run.OK, run.Ms = err == nil, time.Since(t0).Milliseconds()
	if err != nil {
		run.Err = err.Error()
		t.logf("    failed: %v", err)
	}
	return run
}

// setRow sets a sheet row to a text or a raw value.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) setRow(ctx context.Context, id, text string, value any) error {
	body := map[string]any{"row": id}
	if text != "" {
		body["text"] = text
	} else {
		body["value"] = value
	}
	var res uimodel.SetResult
	if err := t.Client.JSON(ctx, http.MethodPost, "/api/sheets/set", body, &res); err != nil {
		return err
	}
	if !res.OK {
		return fmt.Errorf("row %s: %s", id, res.Error)
	}
	return nil
}

// sleep waits d or until ctx ends.
// TRLC-LINKS: REQ-DSO-295
func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// still is what must hold still between two polls for the panel to have
// settled. The sheet values' count is not in it: an open MEAS tab's live
// readings move it with every frame.
type still struct {
	ws, view, mode, tab, row    string
	autoset, detecting, running bool
}

// settle waits until the panel holds still (no sweep, no detection, the
// same sheet values, workspace, view and sheet over two polls) and, while
// running, a frame after that has published.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) settle(ctx context.Context, maxWait time.Duration) error {
	if t.dry() {
		t.logf("    (settle: poll GET /api/status until the panel holds still, then wait for a new frame)")
		return nil
	}
	deadline := time.Now().Add(maxWait)
	sleep(ctx, 200*time.Millisecond)
	var prev still
	stable := 0
	for {
		st, err := readStatus(ctx, t.Client)
		if err != nil {
			return err
		}
		var cur still
		if p := st.Panel; p != nil {
			cur = still{
				ws: p.WorkspaceID, view: p.View, mode: p.Mode, tab: p.Sheet.Tab,
				autoset: p.AutosetBusy || p.Display.AutosetBusy, detecting: p.Display.DecDetecting,
			}
			if p.Sheet.Row != nil {
				cur.row = p.Sheet.Row.ID
			}
		}
		cur.running = st.Running != nil && *st.Running
		if !cur.autoset && !cur.detecting && cur == prev {
			stable++
		} else {
			stable = 0
		}
		prev = cur
		if stable >= 2 {
			if cur.running && st.Seq != nil {
				_, _, _ = t.Client.Do(ctx, http.MethodGet, fmt.Sprintf("/api/frame.bin?v=2&cols=64&since=%d&waitms=%d", *st.Seq, frameWaitMs), nil)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the panel did not settle in %v (autoset %v, detecting %v)", maxWait, cur.autoset, cur.detecting)
		}
		sleep(ctx, 300*time.Millisecond)
	}
}

// startRows are the rows a session starts from, in the order to set them:
// the baseline's, then the session's own (a row the session sets may move
// a baseline row, as a measurement chosen turns the readouts on, and what
// it moves stands); in each, a type or protocol before the rows it makes
// available.
// TRLC-LINKS: REQ-DSO-295
func startRows(s *Session, base Baseline) [][2]string {
	first := func(id string) int {
		switch {
		case strings.HasSuffix(id, ".type"), strings.HasSuffix(id, ".protocol"), strings.HasSuffix(id, ".on"):
			return 0
		case strings.HasPrefix(id, "time."):
			return 2
		}
		return 1
	}
	ordered := func(rows map[string]string) [][2]string {
		ids := slices.Sorted(maps.Keys(rows))
		slices.SortStableFunc(ids, func(a, b string) int { return first(a) - first(b) })
		out := make([][2]string, 0, len(ids))
		for _, id := range ids {
			out = append(out, [2]string{id, rows[id]})
		}
		return out
	}
	baseOnly := map[string]string{}
	for id, text := range base.Rows {
		if _, own := s.Start.Rows[id]; !own {
			baseOnly[id] = text
		}
	}
	return append(ordered(baseOnly), ordered(s.Start.Rows)...)
}

// start brings the scope to the session's start state: no sheet open, the
// start workspace, the start rows (only those that differ: on a real scope
// a row set to what it holds clicks nothing, and a minimal-relay run
// refuses a start that needs a relay), running.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) start(ctx context.Context, s *Session, base Baseline) error {
	t.logf("  start state")
	for range 3 {
		st, err := readStatus(ctx, t.Client)
		if errors.Is(err, ErrPlanned) {
			t.logf("    (while a sheet is open: key menu)")
			break
		}
		if err != nil {
			return err
		}
		if st.Panel == nil || st.Panel.Mode == "workspace" {
			break
		}
		if err := t.Client.Post(ctx, "/api/panel", map[string]any{"button": "menu"}); err != nil {
			return err
		}
		if err := t.settle(ctx, 5*time.Second); err != nil {
			return err
		}
	}
	ws := s.Start.Workspace
	if ws == "" {
		ws = "scope"
	}
	if err := t.Client.Post(ctx, "/api/panel", map[string]any{KindWorkspace: ws}); err != nil && !errors.Is(err, ErrPlanned) {
		return err
	}
	if err := t.settle(ctx, 10*time.Second); err != nil {
		return err
	}
	cur := t.rowTexts(ctx)
	for _, kv := range startRows(s, base) {
		id, text := kv[0], kv[1]
		if have, ok := cur[id]; ok && have == text {
			continue
		}
		if t.MinimalRelay && RelayRow(id) {
			if t.dry() {
				t.logf("    (only if %s already reads %q: a change would click relays)", id, text)
				continue
			}
			return fmt.Errorf("%s reads %q, the session starts at %q: a relay change (minimal-relay run)", id, cur[id], text)
		}
		if t.dry() {
			t.logf("    (only if %s does not read %q already)", id, text)
		}
		if err := t.setRow(ctx, id, text, nil); err != nil && !errors.Is(err, ErrPlanned) {
			return err
		}
		if !t.dry() { // a row may move others (a probe factor scales the V/div)
			cur = t.rowTexts(ctx)
		}
	}
	running := s.Start.Running == nil || *s.Start.Running
	st, err := readStatus(ctx, t.Client)
	if err != nil && !errors.Is(err, ErrPlanned) {
		return err
	}
	if err != nil || st.Running == nil || *st.Running != running {
		v := 0.0
		if running {
			v = 1
		}
		if t.dry() {
			t.logf("    (only if the run state differs)")
		}
		if err := t.Client.Post(ctx, "/api/set", map[string]any{"control": "run", "value": v}); err != nil && !errors.Is(err, ErrPlanned) {
			return err
		}
	}
	return t.settle(ctx, t.settleMax())
}

// settleMax is the target's settle bound.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) settleMax() time.Duration {
	if t.SettleMax > 0 {
		return t.SettleMax
	}
	return 20 * time.Second
}

// rowTexts is every sheet row's text by id; empty on a dry run or when
// the sheet model cannot be read.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) rowTexts(ctx context.Context) map[string]string {
	out := map[string]string{}
	var m uimodel.SheetsModel
	if err := t.Client.JSON(ctx, http.MethodGet, "/api/sheets", nil, &m); err != nil {
		return out
	}
	for _, tab := range m.Tabs {
		for _, r := range tab.Rows {
			if r.Kind != "action" {
				out[r.ID] = r.Text
			}
		}
	}
	return out
}

// savedState is the scope's state before a session, to put back.
type savedState struct {
	workspace string
	rows      map[string]string
	running   bool
}

// save records the workspace, every row and the run state.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) save(ctx context.Context) (*savedState, error) {
	st, err := readStatus(ctx, t.Client)
	if err != nil {
		return nil, err
	}
	s := &savedState{rows: t.rowTexts(ctx), running: st.Running != nil && *st.Running}
	if st.Panel != nil {
		s.workspace = st.Panel.WorkspaceID
	}
	return s, nil
}

// restore puts the recorded state back: every row that changed (twice,
// as a row may wait on another), the workspace and the run state, and
// says what it could not.
// TRLC-LINKS: REQ-DSO-295
func (t *Target) restore(ctx context.Context, sr *Played, before *savedState) {
	if !t.Restore {
		return
	}
	t.logf("  restore the recorded state")
	if before == nil {
		t.logf("    (every row that changed, then the workspace and the run state, as recorded)")
		_ = t.Client.Post(ctx, "/api/sheets/set", map[string]any{"row": "<id>", "text": "<recorded text>"})
		return
	}
	var left []string
	for range 2 {
		left = left[:0]
		cur := t.rowTexts(ctx)
		for _, id := range slices.Sorted(maps.Keys(before.rows)) {
			want := before.rows[id]
			if cur[id] == want {
				continue
			}
			if t.MinimalRelay && RelayRow(id) {
				left = append(left, fmt.Sprintf("%s %q (relay, not restored)", id, want))
				continue
			}
			if err := t.setRow(ctx, id, want, nil); err != nil {
				left = append(left, fmt.Sprintf("%s %q: %v", id, want, err))
			}
			cur = t.rowTexts(ctx) // a row may move others (a probe factor scales the V/div)
		}
	}
	if before.workspace != "" {
		if err := t.Client.Post(ctx, "/api/panel", map[string]any{KindWorkspace: before.workspace}); err != nil {
			left = append(left, "workspace: "+err.Error())
		}
	}
	v := 0.0
	if before.running {
		v = 1
	}
	if err := t.Client.Post(ctx, "/api/set", map[string]any{"control": "run", "value": v}); err != nil {
		left = append(left, "run: "+err.Error())
	}
	_ = t.settle(ctx, t.settleMax())
	cur := t.rowTexts(ctx)
	var diff []string
	for id, want := range before.rows {
		if cur[id] != want {
			diff = append(diff, fmt.Sprintf("%s %q (was %q)", id, cur[id], want))
		}
	}
	slices.Sort(diff)
	switch {
	case len(diff) == 0:
		sr.Restored = "every row as recorded"
	default:
		sr.Restored = "differs: " + strings.Join(diff, "; ")
	}
	if len(left) > 0 {
		sr.Restored += "; not set back: " + strings.Join(left, "; ")
	}
	t.logf("    %s", sr.Restored)
}
