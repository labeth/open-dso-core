package session

import (
	"fmt"
	"io"
	"strings"
)

// cell escapes a value for a Markdown table cell, shortened to n runes.
// TRLC-LINKS: REQ-DSO-295
func cell(s string, n int) string {
	s = strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
	if r := []rune(s); len(r) > n {
		s = string(r[:n-1]) + "…"
	}
	if s == "" {
		return `""`
	}
	return s
}

// Counts are a diff's verdicts.
type Counts struct {
	Match, Diff, NA, Info int
}

// Count counts the diff's verdicts.
// TRLC-LINKS: REQ-DSO-295
func (d Diff) Count() Counts {
	var c Counts
	for _, f := range d.Fields {
		switch f.Verdict {
		case VerdictMatch:
			c.Match++
		case VerdictDiff:
			c.Diff++
		case VerdictNA:
			c.NA++
		case VerdictInfo:
			c.Info++
		}
	}
	return c
}

// WriteMarkdown writes the diff as a table of the fields that differ or
// that one target cannot produce, then the sessions not compared.
// TRLC-LINKS: REQ-DSO-295
func (d Diff) WriteMarkdown(w io.Writer) error {
	c := d.Count()
	var b strings.Builder
	fmt.Fprintf(&b, "## %s vs %s\n\n%d fields match, %d differ, %d cannot be produced by one side, %d reported only.\n\n",
		d.Labels[0], d.Labels[1], c.Match, c.Diff, c.NA, c.Info)
	if c.Diff+c.NA > 0 {
		fmt.Fprintf(&b, "| session | checkpoint | field | %s | %s | tolerance | verdict |\n|---|---|---|---|---|---|---|\n", d.Labels[0], d.Labels[1])
		for _, f := range d.Fields {
			if f.Verdict != VerdictDiff && f.Verdict != VerdictNA {
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n", f.Session, f.Checkpoint, f.Field,
				cell(f.A, 60), cell(f.B, 60), cell(f.Tolerance, 12), f.Verdict)
		}
		b.WriteString("\n")
	}
	if len(d.Missing) > 0 {
		b.WriteString("Not compared:\n\n")
		for _, m := range d.Missing {
			b.WriteString("- " + m + "\n")
		}
		b.WriteString("\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteMarkdown writes the run: each session's steps that failed or could
// not run, its expectations and its restore.
// TRLC-LINKS: REQ-DSO-295
func (r *Run) WriteMarkdown(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n| session | steps ok | expectations ok | failed expectations | notes |\n|---|---|---|---|---|\n", r.Target)
	for _, s := range r.Sessions {
		if s.Skipped != "" {
			fmt.Fprintf(&b, "| %s | - | - | - | skipped: %s |\n", s.Session.Name, cell(s.Skipped, 120))
			continue
		}
		ok := 0
		var notes []string
		for _, st := range s.Steps {
			switch {
			case st.OK:
				ok++
			case st.NA != "":
				notes = append(notes, fmt.Sprintf("%s: n/a (%s)", st.Step, st.NA))
			default:
				notes = append(notes, fmt.Sprintf("%s: %s", st.Step, st.Err))
			}
		}
		eok, ena := 0, 0
		var failed []string
		for _, o := range s.Outcomes {
			switch {
			case o.NA:
				ena++
			case o.OK:
				eok++
			default:
				failed = append(failed, fmt.Sprintf("%s %s %s, got %s", o.Checkpoint, o.Field, o.Expect, o.Got))
			}
		}
		if s.Restored != "" {
			notes = append(notes, "restore: "+s.Restored)
		}
		exp := fmt.Sprintf("%d/%d", eok, len(s.Outcomes))
		if ena > 0 {
			exp += fmt.Sprintf(" (%d n/a)", ena)
		}
		fmt.Fprintf(&b, "| %s | %d/%d | %s | %s | %s |\n", s.Session.Name, ok, len(s.Steps), exp,
			cell(strings.Join(failed, "; "), 400), cell(strings.Join(notes, "; "), 300))
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}
