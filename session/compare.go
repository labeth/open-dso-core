package session

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Tolerance is how two values of a signal-dependent field may differ:
//
//	exact     the same value (the default)
//	abs:X     numbers within X
//	rel:X     numbers within the fraction X of the larger
//	ratio:X   numbers within a factor X (a timebase AUTO picked)
//	set       lists holding the same distinct items, in any order
//	overlap   lists sharing at least one item
//	present   both non-empty, whatever they hold
//	info      reported, never compared
type Tolerance struct {
	Kind string
	X    float64
}

// ParseTolerance reads a tolerance.
// TRLC-LINKS: REQ-DSO-295
func ParseTolerance(t string) (Tolerance, error) {
	k, arg, has := strings.Cut(t, ":")
	switch k {
	case "exact", "set", "overlap", "present", "info":
		if has {
			return Tolerance{}, fmt.Errorf("%q takes no argument", k)
		}
		return Tolerance{Kind: k}, nil
	case "abs", "rel", "ratio":
		x, err := strconv.ParseFloat(arg, 64)
		if err != nil || x < 0 || (k == "ratio" && x < 1) {
			return Tolerance{}, fmt.Errorf("%q: want a number", t)
		}
		return Tolerance{Kind: k, X: x}, nil
	}
	return Tolerance{}, fmt.Errorf("tolerance %q unknown", t)
}

// lookup is field f of s: its own value, else the reason a prefix of it
// could not be produced, else not captured.
// TRLC-LINKS: REQ-DSO-295
func lookup(s Snapshot, f string) (Value, bool) {
	if v, ok := s[f]; ok {
		return v, true
	}
	best := ""
	for k, v := range s {
		if strings.HasSuffix(k, ".") && strings.HasPrefix(f, k) && len(k) > len(best) && v.NA != "" {
			best = k
		}
	}
	if best != "" {
		return s[best], true
	}
	return Value{}, false
}

// listOf is v as a list of strings.
// TRLC-LINKS: REQ-DSO-295
func listOf(v Value) []string {
	switch x := v.V.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			out = append(out, fmt.Sprint(e))
		}
		return out
	case nil:
		return nil
	}
	return []string{v.text()}
}

// numOf is the number v stands for.
// TRLC-LINKS: REQ-DSO-295
func numOf(v Value) (float64, bool) {
	if v.Num != nil {
		return *v.Num, true
	}
	switch x := v.V.(type) {
	case float64:
		return x, true
	case string:
		return parseSI(x)
	}
	return 0, false
}

// Match reports whether a and b agree under tolerance t.
// TRLC-LINKS: REQ-DSO-295
func (t Tolerance) Match(a, b Value) bool {
	switch t.Kind {
	case "info":
		return true
	case "present":
		return a.text() != "" && b.text() != ""
	case "set", "overlap":
		x, y := distinct(listOf(a)), distinct(listOf(b))
		if t.Kind == "set" {
			return slices.Equal(x, y)
		}
		for _, e := range x {
			if slices.Contains(y, e) {
				return true
			}
		}
		return len(x) == 0 && len(y) == 0
	case "abs", "rel", "ratio":
		x, okA := numOf(a)
		y, okB := numOf(b)
		if !okA || !okB {
			return a.text() == b.text()
		}
		switch t.Kind {
		case "abs":
			return math.Abs(x-y) <= t.X
		case "rel":
			return math.Abs(x-y) <= t.X*math.Max(math.Abs(x), math.Abs(y))
		default:
			if x == y {
				return true
			}
			if x <= 0 || y <= 0 {
				return false
			}
			return math.Max(x/y, y/x) <= t.X
		}
	}
	return a.text() == b.text()
}

// distinct is the sorted distinct items of l.
// TRLC-LINKS: REQ-DSO-295
func distinct(l []string) []string {
	x := slices.Clone(l)
	slices.Sort(x)
	return slices.Compact(x)
}

// covered reports whether field f is named by one of names: the name
// itself, or a prefix ending in ".".
// TRLC-LINKS: REQ-DSO-295
func covered(names []string, f string) bool {
	for _, n := range names {
		if n == f || (strings.HasSuffix(n, ".") && strings.HasPrefix(f, n)) {
			return true
		}
	}
	return false
}

// toleranceOf is the tolerance checkpoint c gives field f, by the longest
// name or prefix that names it; exact otherwise.
// TRLC-LINKS: REQ-DSO-295
func (c *Checkpoint) toleranceOf(f string) Tolerance {
	best, bt := "", Tolerance{Kind: "exact"}
	for n, t := range c.Tol {
		if (n == f || (strings.HasSuffix(n, ".") && strings.HasPrefix(f, n))) && len(n) > len(best) {
			if tol, err := ParseTolerance(t); err == nil {
				best, bt = n, tol
			}
		}
	}
	return bt
}

// compared are the fields of checkpoint c compared over snapshots ss:
// the named ones (or DefaultFields) that any snapshot has, plus every
// field with an expectation. A tolerance only says how a compared field
// compares; it does not add fields.
// TRLC-LINKS: REQ-DSO-295
func (c *Checkpoint) compared(ss ...Snapshot) []string {
	names := c.Fields
	if len(names) == 0 {
		names = DefaultFields
	}
	names = append(slices.Clone(names), slices.Collect(maps.Keys(c.Expect))...)
	set := map[string]bool{}
	for _, n := range names {
		if !strings.HasSuffix(n, ".") {
			set[n] = true
		}
		for _, s := range ss {
			for f := range s {
				if !strings.HasSuffix(f, ".") && covered([]string{n}, f) {
					set[f] = true
				}
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

// Verdicts of a field compared between two runs.
const (
	VerdictMatch = "match"
	VerdictDiff  = "DIFF"
	VerdictNA    = "n/a"
	VerdictInfo  = "info"
)

// FieldDiff is one field of one checkpoint compared between two runs.
type FieldDiff struct {
	Session    string `json:"session"`
	Checkpoint string `json:"checkpoint"`
	Field      string `json:"field"`
	A          string `json:"a"`
	B          string `json:"b"`
	Tolerance  string `json:"tolerance,omitempty"`
	Verdict    string `json:"verdict"`
}

// Diff is the comparison of two runs of the same sessions.
type Diff struct {
	A, B   string      `json:"-"`
	Labels [2]string   `json:"targets"`
	Fields []FieldDiff `json:"fields"`
	// Missing are sessions or checkpoints one run has and the other lacks,
	// or skipped, with the reason.
	Missing []string `json:"missing,omitempty"`
}

// shown is a value for a table: its text, or n/a with the reason.
// TRLC-LINKS: REQ-DSO-295
func shown(v Value, ok bool) string {
	switch {
	case !ok:
		return "n/a: not captured"
	case v.NA != "":
		return "n/a: " + v.NA
	}
	return v.text()
}

// Compare compares two runs checkpoint by checkpoint.
// TRLC-LINKS: REQ-DSO-295
func Compare(a, b *Run) Diff {
	d := Diff{Labels: [2]string{a.Target, b.Target}}
	bs := map[string]*Played{}
	for _, s := range b.Sessions {
		bs[s.Session.Name] = s
	}
	for _, sa := range a.Sessions {
		sb := bs[sa.Session.Name]
		delete(bs, sa.Session.Name)
		switch {
		case sb == nil:
			d.Missing = append(d.Missing, fmt.Sprintf("%s: not run on %s", sa.Session.Name, b.Target))
			continue
		case sa.Skipped != "" || sb.Skipped != "":
			d.Missing = append(d.Missing, fmt.Sprintf("%s: skipped (%s: %s; %s: %s)", sa.Session.Name, a.Target, orNone(sa.Skipped), b.Target, orNone(sb.Skipped)))
			continue
		}
		cb := map[string]*CheckRun{}
		for _, c := range sb.Checks {
			cb[c.Name] = c
		}
		for _, ca := range sa.Checks {
			cpb := cb[ca.Name]
			if cpb == nil {
				d.Missing = append(d.Missing, fmt.Sprintf("%s/%s: no checkpoint on %s", sa.Session.Name, ca.Name, b.Target))
				continue
			}
			cp := sa.Session.checkpoint(ca.Name)
			for _, f := range cp.compared(ca.Snapshot, cpb.Snapshot) {
				va, okA := lookup(ca.Snapshot, f)
				vb, okB := lookup(cpb.Snapshot, f)
				if ca.Tainted != "" {
					va, okA = na(ca.Tainted), true
				}
				if cpb.Tainted != "" {
					vb, okB = na(cpb.Tainted), true
				}
				tol := cp.toleranceOf(f)
				fd := FieldDiff{Session: sa.Session.Name, Checkpoint: ca.Name, Field: f, A: shown(va, okA), B: shown(vb, okB)}
				if tol.Kind != "exact" {
					fd.Tolerance = cp.Tol[tolName(cp, f)]
				}
				switch {
				case !okA || !okB || va.NA != "" || vb.NA != "":
					fd.Verdict = VerdictNA
				case tol.Kind == "info":
					fd.Verdict = VerdictInfo
				case tol.Match(va, vb):
					fd.Verdict = VerdictMatch
				default:
					fd.Verdict = VerdictDiff
				}
				d.Fields = append(d.Fields, fd)
			}
		}
	}
	for name := range bs {
		d.Missing = append(d.Missing, fmt.Sprintf("%s: not run on %s", name, a.Target))
	}
	slices.Sort(d.Missing)
	return d
}

// orNone is s, or "ran" when empty.
// TRLC-LINKS: REQ-DSO-295
func orNone(s string) string {
	if s == "" {
		return "ran"
	}
	return s
}

// tolName is the tolerance entry of c that names f.
// TRLC-LINKS: REQ-DSO-295
func tolName(c *Checkpoint, f string) string {
	best := ""
	for n := range c.Tol {
		if (n == f || (strings.HasSuffix(n, ".") && strings.HasPrefix(f, n))) && len(n) > len(best) {
			best = n
		}
	}
	return best
}

// checkpoint is the session's checkpoint named name.
// TRLC-LINKS: REQ-DSO-295
func (s *Session) checkpoint(name string) *Checkpoint {
	for _, st := range s.Steps {
		if st.Check != nil && st.Check.Name == name {
			return st.Check
		}
	}
	return &Checkpoint{Name: name}
}

// Outcome is one expectation evaluated on one run.
type Outcome struct {
	Checkpoint string `json:"checkpoint"`
	Field      string `json:"field"`
	Expect     string `json:"expect"`
	Got        string `json:"got"`
	OK         bool   `json:"ok"`
	NA         bool   `json:"na,omitempty"` // the target could not produce the field
}

// evaluate checks checkpoint c's expectations against snapshot s, with the
// earlier checkpoints' snapshots by name.
// TRLC-LINKS: REQ-DSO-295
func evaluate(c *Checkpoint, s Snapshot, earlier map[string]Snapshot, tainted string) []Outcome {
	var out []Outcome
	for _, f := range slices.Sorted(maps.Keys(c.Expect)) {
		e := c.Expect[f]
		v, ok := lookup(s, f)
		o := Outcome{Checkpoint: c.Name, Field: f, Expect: e.String(), Got: shown(v, ok)}
		switch {
		case tainted != "":
			o.Got, o.NA = "n/a: "+tainted, true
		case !ok || v.NA != "":
			o.NA = true
		default:
			o.OK = e.holds(f, v, earlier)
		}
		out = append(out, o)
	}
	return out
}

// String is the expectation in words.
// TRLC-LINKS: REQ-DSO-295
func (e Expect) String() string {
	switch {
	case e.SameAs != "":
		return "same as at " + e.SameAs
	case e.Differs != "":
		return "differs from " + e.Differs
	case e.Contains != "":
		return fmt.Sprintf("contains %q", e.Contains)
	case e.Ne != nil:
		return fmt.Sprintf("not %v", e.Ne)
	}
	return fmt.Sprintf("= %v", e.Eq)
}

// holds reports whether v, field f's value, meets the expectation.
// TRLC-LINKS: REQ-DSO-295
func (e Expect) holds(f string, v Value, earlier map[string]Snapshot) bool {
	same := func(x any) bool {
		if xf, ok := x.(float64); ok {
			if y, ok := numOf(v); ok {
				return math.Abs(xf-y) <= 1e-9*math.Max(1, math.Abs(xf))
			}
		}
		return fmt.Sprint(x) == v.text()
	}
	switch {
	case e.SameAs != "" || e.Differs != "":
		ref := e.SameAs + e.Differs
		r, ok := lookup(earlier[ref], f)
		eq := ok && r.NA == "" && r.text() == v.text()
		if e.SameAs != "" {
			return eq
		}
		return ok && r.NA == "" && !eq
	case e.Contains != "":
		return strings.Contains(v.text(), e.Contains)
	case e.Ne != nil:
		return !same(e.Ne)
	}
	return same(e.Eq)
}
