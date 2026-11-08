package simdemo_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/scopeapp"
	"github.com/labeth/open-dso-core/simdemo"
)

// The demo composes the product as each scope's device main does, from the
// scope's profile: the same parts with the same values, but where
// Differences says why not. The device compositions are the scopes' own
// (their demotest.Composition keeps them here).
// TRLC-LINKS: REQ-DSO-295
func TestCompositionsAgree(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(simdemo.CompositionsDir(), "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no scope compositions in %s (%v)", simdemo.CompositionsDir(), err)
	}
	used := map[string]bool{}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".json"), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var fx simdemo.CompositionFixture
			if err := json.Unmarshal(raw, &fx); err != nil {
				t.Fatal(err)
			}
			d, err := simdemo.ParseDemo([]byte(fx.Demo))
			if err != nil {
				t.Fatal(err)
			}
			b, err := simdemo.Board([]byte(fx.Profile), d, simdemo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			demo := scopeapp.Describe(b, simdemo.Compose(d, simdemo.Options{Serve: func(_ http.Handler) {}}, b))
			for _, field := range scopeapp.Diff(fx.Device, demo) {
				key, why, ok := reasonOf(field)
				if !ok {
					t.Errorf("%s: the demo composes %s otherwise than the device main, with no stated reason (simdemo.Differences):\n device %s\n demo   %s",
						fx.Scope, field, fieldOf(t, fx.Device, field), fieldOf(t, demo, field))
					continue
				}
				used[key] = true
				t.Logf("%s differs: %s", field, why)
			}
		})
	}
	for _, k := range slices.Sorted(maps.Keys(simdemo.Differences)) {
		if !used[k] {
			t.Errorf("simdemo.Differences states why %s differs, but no scope's composition differs there any more", k)
		}
	}
}

// reasonOf is the stated difference that covers field: its own, or a
// section's ("power_on" covers "power_on.TimeDivS").
func reasonOf(field string) (key, why string, ok bool) {
	for k, w := range simdemo.Differences {
		if (field == k || strings.HasPrefix(field, k+".")) && len(k) > len(key) {
			key, why, ok = k, w, true
		}
	}
	return key, why, ok
}

// fieldOf is a composition's field at path (a section, then its key,
// which may hold dots: "parts.display.engine"), for a failure's message.
func fieldOf(t *testing.T, c scopeapp.Composition, path string) string {
	t.Helper()
	raw, _ := json.Marshal(c)
	var top map[string]json.RawMessage
	_ = json.Unmarshal(raw, &top)
	section, key, _ := strings.Cut(path, ".")
	v := top[section]
	if key != "" {
		var sub map[string]json.RawMessage
		_ = json.Unmarshal(v, &sub)
		v = sub[key]
	}
	if len(v) > 400 {
		return string(v[:400]) + "…"
	}
	return string(v)
}
