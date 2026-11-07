package uimodel

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// rowIDText is a quoted string that looks like a sheet row id.
var rowIDText = regexp.MustCompile("[\"'`]((?:trig|ch[1-4]|time|acq|view|curs|cmp|bus|sys|meas|snd|file)\\.[a-z0-9_]+)[\"'`]")

// Every row id the shared page and the phone remote write in their code is
// in PageRows, and PageRows lists nothing they do not use.
// TRLC-LINKS: REQ-DSO-219, REQ-DSO-220
func TestPageRowsAreThePagesRows(t *testing.T) {
	files, err := filepath.Glob("../webclient/*.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("the web client: %v", err)
	}
	used := map[string]bool{}
	for _, f := range files {
		if strings.Contains(f, ".test.") || strings.Contains(f, "_browser") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range rowIDText.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = true
		}
	}
	for _, chn := range []string{"ch1", "ch2"} { // app_controls.js builds these from the channel number
		used[chn+".bwl"], used[chn+".invert"] = true, true
	}
	for id := range used {
		if _, ok := PageRow(id); !ok {
			t.Errorf("the page uses row %s, which PageRows does not list", id)
		}
	}
	for _, r := range PageRows {
		if !used[r.ID] {
			t.Errorf("PageRows lists %s, which no page uses", r.ID)
		}
	}
}

// CheckRows names a missing row, a wrong kind or unit, a duplicate and a
// row out of use without a reason; a sheet of every row passes.
// TRLC-LINKS: REQ-DSO-219, REQ-DSO-220
func TestCheckRows(t *testing.T) {
	var rows []RowModel
	for _, r := range PageRows {
		rows = append(rows, RowModel{ID: r.ID, Kind: r.Kind, Unit: r.Unit, Available: true})
	}
	m := SheetsModel{Tabs: []TabModel{{ID: "all", Rows: rows}}}
	if p := CheckRows(m); p != nil {
		t.Fatalf("every row: %v", p)
	}
	bad := slices.Clone(rows)
	bad[0].Kind = "number"
	bad[1].Unit = "mV"
	bad[2].Available = false
	bad = append(bad[:4], bad[5:]...)
	bad = append(bad, rows[6])
	p := CheckRows(SheetsModel{Tabs: []TabModel{{Rows: bad}}})
	want := []string{
		PageRows[0].ID + ": kind", PageRows[1].ID + ": unit", PageRows[2].ID + ": out of use",
		PageRows[4].ID + ": missing", PageRows[6].ID + ": listed 2 times",
	}
	if len(p) != len(want) {
		t.Fatalf("problems %v", p)
	}
	for i, w := range want {
		if !strings.HasPrefix(p[i], w) {
			t.Errorf("problem %d: %q, want %q…", i, p[i], w)
		}
	}
	bad[2].Why = "the trigger is not an edge"
	if p := CheckRows(SheetsModel{Tabs: []TabModel{{Rows: bad[2:3]}}}); slices.ContainsFunc(p, func(s string) bool { return strings.HasPrefix(s, PageRows[2].ID+":") }) {
		t.Fatalf("a row out of use with its reason: %v", p)
	}
}
