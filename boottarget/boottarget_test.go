package boottarget

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/fsguard"
	"github.com/labeth/open-dso-core/hal"
)

// TRLC-LINKS: REQ-DSO-297
func TestSetAndNext(t *testing.T) {
	root := t.TempDir()
	g := fsguard.New(fsguard.Policy{Root: root, RootName: "test"})
	dir := filepath.Join(root, "open")
	if Next(dir) != hal.BootOpen {
		t.Fatal("a fresh install boots the open firmware")
	}
	if err := Set(g, dir, hal.BootVendor, "owner"); err != nil {
		t.Fatal(err)
	}
	if Next(dir) != hal.BootVendor {
		t.Fatal("vendor not set")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, DisableName)); string(b) != "owner\n" {
		t.Errorf("disable file %q", b)
	}
	for range 2 {
		if err := Set(g, dir, hal.BootOpen, ""); err != nil || Next(dir) != hal.BootOpen {
			t.Fatalf("open: %v", err)
		}
	}
	if err := Set(g, dir, "x", ""); !errors.Is(err, ErrTarget) {
		t.Errorf("bad target: %v", err)
	}
	if err := Set(g, t.TempDir(), hal.BootVendor, ""); !errors.Is(err, fsguard.ErrRefused) {
		t.Errorf("outside the root: %v", err)
	}
	for in, want := range map[string]string{"OPEN": hal.BootOpen, "vend": hal.BootVendor, "Vendor\n": hal.BootVendor} {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("parse %q: %q %v", in, got, err)
		}
	}
	if _, err := Parse("on"); err == nil {
		t.Error("parse on accepted")
	}
}

// TRLC-LINKS: REQ-DSO-297
func TestExtension(t *testing.T) {
	root := t.TempDir()
	sw := Switch{Guard: fsguard.New(fsguard.Policy{Root: root}), Dir: filepath.Join(root, "open")}
	x := Extension(sw)
	if x.Feature != Feature || len(x.SCPI) != 1 || len(x.Routes) != 2 {
		t.Fatalf("extension %+v", x)
	}
	h := x.SCPI[0]
	if err := h.Command("VEND"); err != nil || sw.Next() != hal.BootVendor {
		t.Fatalf("SCPI VEND: %v, next %s", err, sw.Next())
	}
	if b, _ := os.ReadFile(filepath.Join(sw.Dir, DisableName)); string(b) != "asked through SCPI\n" {
		t.Errorf("disable file %q", b)
	}
	if r, err := h.Query(""); err != nil || string(r) != "VENDOR" {
		t.Errorf("SCPI query %q %v", r, err)
	}
	if err := h.Command("ON"); err == nil {
		t.Error("SCPI ON accepted")
	}
	mux := http.NewServeMux()
	for _, r := range x.Routes {
		mux.Handle(r.Pattern, r.Handler)
	}
	do := func(method, target, body string) (int, string) {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body)))
		return rec.Code, strings.TrimSpace(rec.Body.String())
	}
	if c, b := do(http.MethodPost, "/api/boot?next=open", ""); c != http.StatusOK || b != `{"next":"open"}` {
		t.Errorf("POST open: %d %s", c, b)
	}
	if c, b := do(http.MethodPost, "/api/boot", "vendor"); c != http.StatusOK || b != `{"next":"vendor"}` {
		t.Errorf("POST vendor body: %d %s", c, b)
	}
	if c, _ := do(http.MethodPost, "/api/boot", "maybe"); c != http.StatusBadRequest {
		t.Errorf("POST maybe: %d", c)
	}
	if c, b := do(http.MethodGet, "/api/boot", ""); c != http.StatusOK || b != `{"next":"vendor"}` {
		t.Errorf("GET: %d %s", c, b)
	}
}
