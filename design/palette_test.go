package design

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestPaletteParity holds both generated files to tokens.json: the web's
// tokens.css and the displays' Tokens. It fails when tokens.json changed
// without `go generate ./design`, so a scope's web UI and its display cannot
// diverge.
func TestPaletteParity(t *testing.T) {
	raw, err := os.ReadFile("tokens.json")
	if err != nil {
		t.Fatal(err)
	}
	var src struct {
		Tokens map[string]string `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &src); err != nil {
		t.Fatal(err)
	}
	css, err := fs.ReadFile(CSS, "tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{}
	for _, m := range regexp.MustCompile(`--([\w-]+):\s*(#[0-9a-fA-F]{6})`).FindAllStringSubmatch(string(css), -1) {
		vars[m[1]] = strings.ToLower(m[2])
	}
	if len(vars) != len(src.Tokens) || len(Tokens) != len(src.Tokens) {
		t.Fatalf("tokens.json has %d tokens, tokens.css %d, Tokens %d: run go generate ./design", len(src.Tokens), len(vars), len(Tokens))
	}
	for name, hex := range src.Tokens {
		hex = strings.ToLower(hex)
		if vars[name] != hex {
			t.Errorf("tokens.css --%s = %q, tokens.json %q", name, vars[name], hex)
		}
		c := Tokens[name]
		if got := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B); got != hex {
			t.Errorf("Tokens[%q] = %s, tokens.json %s", name, got, hex)
		}
	}
}

// TestSharedStylesheets checks the stylesheets every scope serves are here.
func TestSharedStylesheets(t *testing.T) {
	for _, f := range []string{"tokens.css", "base.css", "remote.css", "sound.css"} {
		if _, err := fs.Stat(CSS, f); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}
