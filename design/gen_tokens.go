//go:build ignore

// Generates the shared palette from tokens.json into two files, so a scope's
// web UI and its display share one source of truth:
//   - tokens.css      : the web :root custom properties
//   - palette_gen.go  : Tokens, the same colours for displays
//
// Run: go generate ./design   (or: cd design && go run gen_tokens.go)
// TestPaletteParity asserts both files match tokens.json.
package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"slices"
	"strings"
)

type spec struct {
	Tokens map[string]string `json:"tokens"`
}

func hexRGB(h string) (r, g, b uint8) {
	h = strings.TrimPrefix(h, "#")
	var v uint64
	fmt.Sscanf(h, "%06x", &v)
	return uint8(v >> 16), uint8(v >> 8), uint8(v)
}

func main() {
	raw, err := os.ReadFile("tokens.json")
	must(err)
	var s spec
	must(json.Unmarshal(raw, &s))

	names := make([]string, 0, len(s.Tokens))
	for n := range s.Tokens {
		names = append(names, n)
	}
	slices.Sort(names)

	// tokens.css — :root vars, sorted for deterministic output.
	var css strings.Builder
	css.WriteString("/* Code generated from tokens.json by gen_tokens.go. DO NOT EDIT. */\n:root {\n")
	for _, n := range names {
		fmt.Fprintf(&css, "  --%s: %s;\n", n, strings.ToLower(s.Tokens[n]))
	}
	css.WriteString("}\n")
	must(os.WriteFile("tokens.css", []byte(css.String()), 0o644))

	// palette_gen.go — Tokens by name.
	var g strings.Builder
	g.WriteString("// Code generated from tokens.json by gen_tokens.go. DO NOT EDIT.\n\n")
	g.WriteString("package design\n\n// Tokens are the palette's colours by token name (tokens.css's --name).\nvar Tokens = map[string]RGB{\n")
	for _, n := range names {
		r, gg, b := hexRGB(s.Tokens[n])
		fmt.Fprintf(&g, "\t%q: {%d, %d, %d}, // %s\n", n, r, gg, b, strings.ToLower(s.Tokens[n]))
	}
	g.WriteString("}\n")
	formatted, err := format.Source([]byte(g.String()))
	must(err)
	must(os.WriteFile("palette_gen.go", formatted, 0o644))

	fmt.Printf("generated tokens.css + palette_gen.go (%d tokens)\n", len(names))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
