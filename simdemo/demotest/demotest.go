// Package demotest holds the checks a scope's repository runs to keep its
// demo aligned with its device: its device main's composition kept beside
// the core's demo (Composition), the demo's front panel against the
// board's key table (Panel), and recorded captures replayed through the
// demo (in package simdemo).
//
// A check that compares against a kept file rewrites it when the
// environment has UPDATE_DEMO_FIXTURES=1.
package demotest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/keytable"
	"github.com/labeth/open-dso-core/scopeapp"
	"github.com/labeth/open-dso-core/simdemo"
)

// Update reports whether kept files are rewritten.
// TRLC-LINKS: REQ-DSO-298
func Update() bool { return os.Getenv("UPDATE_DEMO_FIXTURES") == "1" }

// Composition checks that the core's kept composition of the scope name
// is its device main's: options builds the device main's Options over a
// board (asked only for its profile and planner: the simulated board of
// the scope's profile and demo file), and the kept file holds the
// description with the profile and demo file the core's demo test builds
// the demo's composition from (simdemo.TestCompositionsAgree).
// TRLC-LINKS: REQ-DSO-298
func Composition(t *testing.T, name string, profileYAML, demoJSON []byte, options func(b hal.Board) scopeapp.Options) {
	t.Helper()
	d, err := simdemo.ParseDemo(demoJSON)
	if err != nil {
		t.Fatal(err)
	}
	b, err := simdemo.Board(profileYAML, d, simdemo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := simdemo.CompositionFixture{
		Scope: name, Profile: string(profileYAML), Demo: string(demoJSON),
		Device: scopeapp.Describe(b, options(b)),
	}
	keep(t, filepath.Join(simdemo.CompositionsDir(), name+".json"), want)
}

// Panel checks the scope's demo file against its board's key table: the
// table the demo file carries is the board's (kept: UPDATE_DEMO_FIXTURES=1
// rewrites demoPath's "keytable"), it names exactly the profile's keys and
// knobs, and the page's layout places every control of the table and
// nothing else.
// TRLC-LINKS: REQ-DSO-298
func Panel(t *testing.T, demoPath string, profile hal.InputSpec, table keytable.Table) {
	t.Helper()
	raw, err := os.ReadFile(demoPath) //nolint:gosec // G304: the scope's own demo file
	if err != nil {
		t.Fatal(err)
	}
	d, err := simdemo.ParseDemo(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !d.KeyTable.Equal(table) {
		if !Update() {
			t.Fatalf("%s: the demo's key table is not the board's (UPDATE_DEMO_FIXTURES=1 rewrites it):\n%s", demoPath, d.KeyTable.DiffFrom(table))
		}
		out, err := simdemo.WithKeyTable(raw, table)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(demoPath, out, 0o644); err != nil { //nolint:gosec // G306: a repository file
			t.Fatal(err)
		}
		d.KeyTable = table
	}
	if err := table.CheckProfile(profile); err != nil {
		t.Errorf("the board's key table against the profile: %v", err)
	}
	if err := d.CheckPanel(); err != nil {
		t.Errorf("%s: %v", demoPath, err)
	}
}

// keep compares v with the JSON kept at path, or writes it.
// TRLC-LINKS: REQ-DSO-298
func keep(t *testing.T, path string, v any) {
	t.Helper()
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	old, err := os.ReadFile(path) //nolint:gosec // G304: the core's kept file
	if err == nil && bytes.Equal(old, got) {
		return
	}
	if !Update() {
		t.Fatalf("%s is not this scope's device composition (UPDATE_DEMO_FIXTURES=1 rewrites it; then run the core's simdemo tests)", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: a repository directory
		t.Fatal(err)
	}
	if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // G306: a repository file
		t.Fatal(err)
	}
}
