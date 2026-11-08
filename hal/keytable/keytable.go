// Package keytable is a front panel's key table as data: each physical key
// and knob by the panel's own name, the UI model's name its events carry,
// and the shape of those events (a press alone or a press and a release; a
// knob's steps an event). A scope's board states its table from its own
// mapping, the simulated board takes the events its bench page asks for
// through it, and a test holds the two to one table, so the demo's panel
// sends what the real panel sends.
package keytable

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/labeth/open-dso-core/hal"
)

// Chan is the place in a name the selected channel's number takes: a panel
// with one set of vertical knobs for all its channels sends the name of
// the channel whose key was pressed last.
const Chan = "{ch}"

// Table is a panel's keys and knobs.
type Table struct {
	Keys  []Key  `json:"keys"`
	Knobs []Knob `json:"knobs"`
}

// Key is one key or knob push.
type Key struct {
	// Panel is the key's name on this panel (the board's own).
	Panel string `json:"panel"`
	// Name is the UI model's key its events carry; "" for a key the board
	// gives no name (its events are dropped).
	Name string `json:"name,omitempty"`
	// Release: the board reports the key's release as well as its press.
	Release bool `json:"release,omitempty"`
	// Selects is the channel (1-based) a press selects for Chan.
	Selects int `json:"selects,omitempty"`
}

// Knob is one rotary control.
type Knob struct {
	Panel string `json:"panel"`
	Name  string `json:"name"`
	// Max is the most detents one event carries (1: an event a detent);
	// Steps, when set, the only magnitudes an event carries (an
	// accelerated knob's).
	Max   int   `json:"max,omitempty"`
	Steps []int `json:"steps,omitempty"`
}

// FromProfile is the table of a panel known only by its profile: every key
// and knob named as the UI model names it, a key pressed and released, a
// knob up to 100 detents an event.
// TRLC-LINKS: REQ-DSO-1001
func FromProfile(in hal.InputSpec) Table {
	var t Table
	for _, k := range in.Keys {
		t.Keys = append(t.Keys, Key{Panel: k, Name: k, Release: true})
	}
	for _, k := range in.Knobs {
		t.Knobs = append(t.Knobs, Knob{Panel: k, Name: k, Max: 100})
	}
	return t
}

// Empty reports whether t has no controls.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Empty() bool { return len(t.Keys) == 0 && len(t.Knobs) == 0 }

// Key is the key named panel on this panel.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Key(panel string) (Key, bool) {
	i := slices.IndexFunc(t.Keys, func(k Key) bool { return k.Panel == panel })
	if i < 0 {
		return Key{}, false
	}
	return t.Keys[i], true
}

// Knob is the knob named panel on this panel.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Knob(panel string) (Knob, bool) {
	i := slices.IndexFunc(t.Knobs, func(k Knob) bool { return k.Panel == panel })
	if i < 0 {
		return Knob{}, false
	}
	return t.Knobs[i], true
}

// Expand is name with the selected channel sel in place of Chan.
// TRLC-LINKS: REQ-DSO-1001
func Expand(name string, sel int) string {
	return strings.ReplaceAll(name, Chan, strconv.Itoa(max(sel, 1)))
}

// Channels is the most channels the table's keys select (1 without any).
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Channels() int {
	n := 1
	for _, k := range t.Keys {
		n = max(n, k.Selects)
	}
	return n
}

// Press is the events a press of key k sends with channel sel selected,
// and the channel selected after it.
// TRLC-LINKS: REQ-DSO-1001
func (k Key) Press(sel int) ([]hal.InputEvent, int) {
	if k.Selects > 0 {
		sel = k.Selects
	}
	if k.Name == "" {
		return nil, sel
	}
	name := Expand(k.Name, sel)
	evs := []hal.InputEvent{{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: name, Down: true}}
	if k.Release {
		evs = append(evs, hal.InputEvent{Source: hal.FromPanel, Kind: hal.KeyEvent, Key: name})
	}
	return evs, sel
}

// Turn is the events detents of knob k send with channel sel selected:
// one detent an event on a knob of Max 1, as many as an event carries on
// the others, an accelerated knob's in the magnitudes it reports, largest
// first.
// TRLC-LINKS: REQ-DSO-1001
func (k Knob) Turn(detents, sel int) []hal.InputEvent {
	sign, left := 1, detents
	if left < 0 {
		sign, left = -1, -left
	}
	name := Expand(k.Name, sel)
	var evs []hal.InputEvent
	for left > 0 {
		n := k.step(left)
		evs = append(evs, hal.InputEvent{Source: hal.FromPanel, Kind: hal.KnobEvent, Knob: name, Steps: sign * n})
		left -= n
	}
	return evs
}

// step is the detents the next event of a turn with left detents to go
// carries.
// TRLC-LINKS: REQ-DSO-1001
func (k Knob) step(left int) int {
	if len(k.Steps) > 0 {
		best := 0
		for _, s := range k.Steps {
			if s <= left && s > best {
				best = s
			}
		}
		return max(best, 1)
	}
	return min(left, max(k.Max, 1))
}

// takes reports whether one event of k carries n detents.
// TRLC-LINKS: REQ-DSO-1001
func (k Knob) takes(n int) bool {
	if n < 0 {
		n = -n
	}
	if len(k.Steps) > 0 {
		return slices.Contains(k.Steps, n)
	}
	return n >= 1 && n <= max(k.Max, 1)
}

// KeyNamed is the key whose events carry the UI model's key name with
// channel sel selected (the first, where several do).
// TRLC-LINKS: REQ-DSO-1001
func (t Table) KeyNamed(name string, sel int) (Key, error) {
	other := false
	for _, k := range t.Keys {
		switch {
		case k.Name == "":
		case Expand(k.Name, sel) == name:
			return k, nil
		case strings.Contains(k.Name, Chan) && matchesAnyChannel(k.Name, name, t.Channels()):
			other = true
		}
	}
	if other {
		return Key{}, fmt.Errorf("%s: this panel's key acts on the selected channel, C%d; press the channel's key first", name, max(sel, 1))
	}
	return Key{}, fmt.Errorf("%s: this panel has no key that sends it", name)
}

// KnobNamed is the knob whose events carry the UI model's knob name with
// channel sel selected.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) KnobNamed(name string, sel int) (Knob, error) {
	other := false
	for _, k := range t.Knobs {
		switch {
		case Expand(k.Name, sel) == name:
			return k, nil
		case strings.Contains(k.Name, Chan) && matchesAnyChannel(k.Name, name, t.Channels()):
			other = true
		}
	}
	if other {
		return Knob{}, fmt.Errorf("%s: this panel's knob acts on the selected channel, C%d; press the channel's key first", name, max(sel, 1))
	}
	return Knob{}, fmt.Errorf("%s: this panel has no knob that sends it", name)
}

// matchesAnyChannel reports whether template sends name for some channel
// up to n.
// TRLC-LINKS: REQ-DSO-1001
func matchesAnyChannel(template, name string, n int) bool {
	for c := 1; c <= n; c++ {
		if Expand(template, c) == name {
			return true
		}
	}
	return false
}

// Check reports whether the board could send ev with channel sel selected:
// a name, kind and press/release or step shape its table has.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Check(ev hal.InputEvent, sel int) error {
	switch ev.Kind {
	case hal.KeyEvent:
		k, err := t.KeyNamed(ev.Key, sel)
		if err != nil {
			return err
		}
		if !ev.Down && !k.Release {
			return fmt.Errorf("%s: this panel reports the press only, never the release", ev.Key)
		}
		return nil
	case hal.KnobEvent:
		k, err := t.KnobNamed(ev.Knob, sel)
		if err != nil {
			return err
		}
		if !k.takes(ev.Steps) {
			return fmt.Errorf("%s: an event of this knob never carries %d detents", ev.Knob, ev.Steps)
		}
		return nil
	case hal.TouchEvent, hal.Overload:
		return nil
	}
	return fmt.Errorf("an input event of kind %d", ev.Kind)
}

// Names are the UI model's key and knob names the table's events carry,
// over every channel its keys select.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Names() (keys, knobs []string) {
	n := t.Channels()
	add := func(dst []string, name string) []string {
		if name == "" {
			return dst
		}
		if !strings.Contains(name, Chan) {
			return append(dst, name)
		}
		for c := 1; c <= n; c++ {
			dst = append(dst, Expand(name, c))
		}
		return dst
	}
	for _, k := range t.Keys {
		keys = add(keys, k.Name)
	}
	for _, k := range t.Knobs {
		knobs = add(knobs, k.Name)
	}
	slices.Sort(keys)
	slices.Sort(knobs)
	return slices.Compact(keys), slices.Compact(knobs)
}

// Validate refuses a table with a control named twice or a knob without
// its step shape.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Validate() error {
	seen := map[string]bool{}
	var errs []error
	for _, k := range t.Keys {
		if k.Panel == "" || seen[k.Panel] {
			errs = append(errs, fmt.Errorf("key %q: empty or named twice", k.Panel))
		}
		seen[k.Panel] = true
	}
	for _, k := range t.Knobs {
		if k.Panel == "" || seen[k.Panel] {
			errs = append(errs, fmt.Errorf("knob %q: empty or named twice", k.Panel))
		}
		seen[k.Panel] = true
		if k.Name == "" || (k.Max < 1 && len(k.Steps) == 0) {
			errs = append(errs, fmt.Errorf("knob %q: no name or no step shape", k.Panel))
		}
	}
	return errors.Join(errs...)
}

// CheckProfile reports where the table and a profile's inputs disagree: a
// name the table sends that the profile lacks, or a profile key or knob no
// control sends.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) CheckProfile(in hal.InputSpec) error {
	keys, knobs := t.Names()
	var errs []error
	cmp := func(what string, sent, listed []string) {
		for _, n := range sent {
			if !slices.Contains(listed, n) {
				errs = append(errs, fmt.Errorf("the panel sends %s %q, which the profile does not list", what, n))
			}
		}
		for _, n := range listed {
			if !slices.Contains(sent, n) {
				errs = append(errs, fmt.Errorf("the profile lists %s %q, which no control of the panel sends", what, n))
			}
		}
	}
	cmp("key", keys, in.Keys)
	cmp("knob", knobs, in.Knobs)
	return errors.Join(append([]error{t.Validate()}, errs...)...)
}

// Equal reports whether t and o are the same table.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) Equal(o Table) bool {
	return slices.Equal(t.Keys, o.Keys) && slices.EqualFunc(t.Knobs, o.Knobs, func(a, b Knob) bool {
		return a.Panel == b.Panel && a.Name == b.Name && a.Max == b.Max && slices.Equal(a.Steps, b.Steps)
	})
}

// DiffFrom lists the controls where t differs from want.
// TRLC-LINKS: REQ-DSO-1001
func (t Table) DiffFrom(want Table) string {
	var b strings.Builder
	for _, k := range want.Keys {
		if got, ok := t.Key(k.Panel); !ok || got != k {
			fmt.Fprintf(&b, "key %s: have %+v, the board %+v\n", k.Panel, got, k)
		}
	}
	for _, k := range t.Keys {
		if _, ok := want.Key(k.Panel); !ok {
			fmt.Fprintf(&b, "key %s: the board has no such key\n", k.Panel)
		}
	}
	for _, k := range want.Knobs {
		if got, ok := t.Knob(k.Panel); !ok || got.Name != k.Name || got.Max != k.Max || !slices.Equal(got.Steps, k.Steps) {
			fmt.Fprintf(&b, "knob %s: have %+v, the board %+v\n", k.Panel, got, k)
		}
	}
	for _, k := range t.Knobs {
		if _, ok := want.Knob(k.Panel); !ok {
			fmt.Fprintf(&b, "knob %s: the board has no such knob\n", k.Panel)
		}
	}
	return b.String()
}
