package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Session is one recorded session: the signals on the inputs, the state it
// starts from, and its steps.
type Session struct {
	Name  string `json:"name"`
	About string `json:"about"`
	// Relays says that a step changes the analog front end (a V/div,
	// coupling or probe change, Default Setup, an AUTO sweep), which clicks
	// relays on a real scope; a minimal-relay run skips the session.
	Relays bool `json:"relays"`
	// Needs are target abilities the session cannot run without:
	// "board-input" (keys pressed through the board's own panel queue).
	Needs []string `json:"needs,omitempty"`
	// Signals are the inputs' signals, by input name ("C1"). An input not
	// named is left as the target has it.
	Signals map[string]Signal `json:"signals"`
	// Start is the state the session starts from: the baseline, overlaid
	// with these sheet rows (by row id, as their text).
	Start Start   `json:"start"`
	Steps []*Step `json:"steps"`
}

// Start is a session's starting state.
type Start struct {
	Workspace string            `json:"workspace,omitempty"` // default "scope"
	Rows      map[string]string `json:"rows,omitempty"`      // row id → text, over the baseline's
	Running   *bool             `json:"running,omitempty"`   // default running
}

// Step is one action, a wait or a checkpoint; exactly one of its action
// fields is set.
type Step struct {
	// Key presses a front-panel key by its UI-model name ("ch1", "auto").
	// Via says through what: "ui" (the default) injects it into the UI
	// model as the scope's own panel does (/api/panel); "board" queues it
	// on the board's panel input, as a hand on the bench page does.
	Key  string `json:"key,omitempty"`
	Via  string `json:"via,omitempty"`
	Knob string `json:"knob,omitempty"`
	// Steps are the knob's signed detents.
	Steps int `json:"steps,omitempty"`
	// Workspace switches the workspace the way the web remote does.
	Workspace string `json:"workspace,omitempty"`
	// Row sets a sheet row the way the web remote does.
	Row *RowSet `json:"row,omitempty"`
	// Set applies a web control (/api/set).
	Set *Control `json:"set,omitempty"`
	// Signals change inputs' signals mid-session.
	Signals map[string]Signal `json:"signals,omitempty"`
	// WaitS waits this many seconds.
	WaitS float64 `json:"wait_s,omitempty"`
	// Check is a checkpoint.
	Check *Checkpoint `json:"check,omitempty"`
}

// RowSet is one sheet row set by id, to a text or a raw value.
type RowSet struct {
	ID    string `json:"id"`
	Text  string `json:"text,omitempty"`
	Value any    `json:"value,omitempty"`
}

// Control is one /api/set control.
type Control struct {
	Control string  `json:"control"`
	Value   float64 `json:"value"`
	Ch      *int    `json:"ch,omitempty"`
}

// Checkpoint captures the scope's state under a name.
type Checkpoint struct {
	Name string `json:"name"`
	// Fields are the fields compared between targets, as field names or
	// prefixes ending in "."; empty: DefaultFields.
	Fields []string `json:"fields,omitempty"`
	// Tol are tolerances by field name or prefix, for fields that depend
	// on the signal (see Tolerance).
	Tol map[string]string `json:"tol,omitempty"`
	// Expect are what each target must show, by field.
	Expect map[string]Expect `json:"expect,omitempty"`
}

// Expect is one expectation of a field; one of its members is set.
type Expect struct {
	Eq       any    `json:"eq,omitempty"`       // equal to this value (a number within 1e-9)
	Ne       any    `json:"ne,omitempty"`       // anything but this value
	SameAs   string `json:"same_as,omitempty"`  // equal to the field at an earlier checkpoint
	Differs  string `json:"differs,omitempty"`  // differs from the field at an earlier checkpoint
	Contains string `json:"contains,omitempty"` // the text holds this
}

// The step kinds, as Kind names them.
const (
	KindKey       = "key"
	KindKnob      = "knob"
	KindWorkspace = "workspace"
	KindRow       = "row"
	KindSet       = "set"
	KindSignals   = "signals"
	KindWait      = "wait"
	KindCheck     = "check"
)

// Kind is the step's kind; "" when no or several action fields are set.
// TRLC-LINKS: REQ-DSO-295
func (s *Step) Kind() string {
	var kinds []string
	add := func(on bool, k string) {
		if on {
			kinds = append(kinds, k)
		}
	}
	add(s.Key != "", KindKey)
	add(s.Knob != "", KindKnob)
	add(s.Workspace != "", KindWorkspace)
	add(s.Row != nil, KindRow)
	add(s.Set != nil, KindSet)
	add(len(s.Signals) > 0, KindSignals)
	add(s.WaitS > 0, KindWait)
	add(s.Check != nil, KindCheck)
	if len(kinds) != 1 {
		return ""
	}
	return kinds[0]
}

// String is the step as one line, for logs and reports.
// TRLC-LINKS: REQ-DSO-295
func (s *Step) String() string {
	switch s.Kind() {
	case KindKey:
		if s.Via == ViaBoard {
			return "key " + s.Key + " (board panel)"
		}
		return "key " + s.Key
	case KindKnob:
		return fmt.Sprintf("knob %s %+d", s.Knob, s.Steps)
	case KindWorkspace:
		return "workspace " + s.Workspace + " (web)"
	case KindRow:
		if s.Row.Text != "" {
			return fmt.Sprintf("row %s = %q (web)", s.Row.ID, s.Row.Text)
		}
		return fmt.Sprintf("row %s = %v (web)", s.Row.ID, s.Row.Value)
	case KindSet:
		if s.Set.Ch != nil {
			return fmt.Sprintf("set %s C%d = %g (web)", s.Set.Control, *s.Set.Ch+1, s.Set.Value)
		}
		return fmt.Sprintf("set %s = %g (web)", s.Set.Control, s.Set.Value)
	case KindSignals:
		return "signals " + DescribeSignals(s.Signals)
	case KindWait:
		return fmt.Sprintf("wait %gs", s.WaitS)
	case KindCheck:
		return "check " + s.Check.Name
	}
	return "invalid step"
}

// The ways a key reaches the scope.
const (
	ViaUI    = "ui"
	ViaBoard = "board"
)

// NeedBoardInput is the Needs entry of a session that presses keys
// through the board's panel queue.
const NeedBoardInput = "board-input"

// relayKeys are keys whose action changes the analog front end.
var relayKeys = map[string]bool{"auto": true, "defaultsetup": true}

// relayKnob reports whether a knob changes the analog front end.
// TRLC-LINKS: REQ-DSO-295
func relayKnob(name string) bool { return strings.HasSuffix(name, "vdiv") }

// RelayRow reports whether a sheet row's change switches front-end relays:
// a channel's V/div, coupling, probe or bandwidth limit.
// TRLC-LINKS: REQ-DSO-295
func RelayRow(id string) bool {
	ch, field, ok := strings.Cut(id, ".")
	if !ok || !strings.HasPrefix(ch, "ch") {
		return false
	}
	switch field {
	case "vdiv", "coupling", "probe", "bwl":
		return true
	}
	return false
}

// relayControls are the /api/set controls that switch front-end relays.
var relayControls = map[string]bool{"vdiv": true, "coupling": true, "probe": true, "bwlimit": true}

// ClicksRelays reports whether the step changes the analog front end.
// TRLC-LINKS: REQ-DSO-295
func (s *Step) ClicksRelays() bool {
	switch s.Kind() {
	case KindKey:
		return relayKeys[s.Key]
	case KindKnob:
		return relayKnob(s.Knob)
	case KindRow:
		return RelayRow(s.Row.ID)
	case KindSet:
		return relayControls[s.Set.Control]
	}
	return false
}

// Validate reports a session that cannot run: a step without exactly one
// action, a checkpoint without a name or named twice, an expectation on
// a later checkpoint, a relay step the session does not declare.
// TRLC-LINKS: REQ-DSO-295
func (s *Session) Validate() error {
	var errs []error
	if s.Name == "" {
		errs = append(errs, errors.New("no name"))
	}
	seen := map[string]bool{}
	relays := false
	for i, st := range s.Steps {
		k := st.Kind()
		if k == "" {
			errs = append(errs, fmt.Errorf("step %d: want exactly one action", i+1))
			continue
		}
		relays = relays || st.ClicksRelays()
		if st.Via != "" && st.Via != ViaUI && st.Via != ViaBoard {
			errs = append(errs, fmt.Errorf("step %d: via %q: want ui or board", i+1, st.Via))
		}
		if st.Via == ViaBoard && !slices.Contains(s.Needs, NeedBoardInput) {
			errs = append(errs, fmt.Errorf("step %d: a board key needs %q in needs", i+1, NeedBoardInput))
		}
		for in, sig := range st.Signals {
			if err := sig.Validate(); err != nil {
				errs = append(errs, fmt.Errorf("step %d: %s: %w", i+1, in, err))
			}
		}
		if k != KindCheck {
			continue
		}
		c := st.Check
		if c.Name == "" || seen[c.Name] {
			errs = append(errs, fmt.Errorf("step %d: checkpoint name %q empty or used twice", i+1, c.Name))
		}
		for f, e := range c.Expect {
			for _, ref := range []string{e.SameAs, e.Differs} {
				if ref != "" && !seen[ref] {
					errs = append(errs, fmt.Errorf("checkpoint %s: %s refers to %q, not an earlier checkpoint", c.Name, f, ref))
				}
			}
		}
		for f, tol := range c.Tol {
			if _, err := ParseTolerance(tol); err != nil {
				errs = append(errs, fmt.Errorf("checkpoint %s: tolerance of %s: %w", c.Name, f, err))
			}
		}
		seen[c.Name] = true
	}
	if relays != s.Relays {
		errs = append(errs, fmt.Errorf("relays is %v, the steps say %v", s.Relays, relays))
	}
	for in, sig := range s.Signals {
		if err := sig.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("signal %s: %w", in, err))
		}
	}
	if len(seen) == 0 {
		errs = append(errs, errors.New("no checkpoint"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("session %s: %w", s.Name, err)
	}
	return nil
}

// Parse reads one session file's bytes, strictly: an unknown field is an
// error.
// TRLC-LINKS: REQ-DSO-295
func Parse(data []byte) (*Session, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Session
	if err := dec.Decode(&s); err != nil {
		return nil, err
	}
	return &s, s.Validate()
}

// Baseline is the state every session starts from before its own Start:
// the rows a session names override these.
type Baseline struct {
	Rows map[string]string `json:"rows"`
}

// BaselineFile is the baseline's file name in a sessions directory.
const BaselineFile = "baseline.json"

// Load reads every session in dir (each *.json but the baseline), sorted
// by name, and the baseline.
// TRLC-LINKS: REQ-DSO-295
func Load(dir string) ([]*Session, Baseline, error) {
	var base Baseline
	b, err := os.ReadFile(filepath.Join(dir, BaselineFile)) //nolint:gosec // G304: the sessions directory the user names
	if err != nil {
		return nil, base, err
	}
	if err := json.Unmarshal(b, &base); err != nil {
		return nil, base, fmt.Errorf("%s: %w", BaselineFile, err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, base, err
	}
	var out []*Session
	var errs []error
	for _, f := range files {
		if filepath.Base(f) == BaselineFile {
			continue
		}
		data, err := os.ReadFile(f) //nolint:gosec // G304: a file of the sessions directory
		if err != nil {
			return nil, base, err
		}
		s, err := Parse(data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Base(f), err))
			continue
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b *Session) int { return strings.Compare(a.Name, b.Name) })
	return out, base, errors.Join(errs...)
}
