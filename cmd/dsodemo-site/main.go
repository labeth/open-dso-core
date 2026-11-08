// Command dsodemo-site builds the browser demo of a scope as a static site
// (simdemo.BuildSite): the demo's WebAssembly module, Go's wasm_exec.js, the
// bench page and its workers, the shared stylesheets and the scope's
// profile and demo file.
//
//	go run ./cmd/dsodemo-site -profile P -demo D -out DIR
//
// The site needs nothing but a static file server; its service worker needs
// a secure context (https, or http on localhost).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/labeth/open-dso-core/simdemo"
)

// TRLC-LINKS: REQ-DSO-298
func main() {
	profilePath := flag.String("profile", "", "the scope's profile.yaml")
	demoPath := flag.String("demo", "", "the scope's demo.json")
	out := flag.String("out", "", "the site's directory (created; its files are replaced)")
	flag.Parse()
	if err := build(*profilePath, *demoPath, *out); err != nil {
		fmt.Fprintln(os.Stderr, "dsodemo-site:", err)
		os.Exit(1)
	}
}

// TRLC-LINKS: REQ-DSO-298
func build(profilePath, demoPath, out string) error {
	if profilePath == "" || demoPath == "" || out == "" {
		return errors.New("-profile, -demo and -out are required")
	}
	profileYAML, err := os.ReadFile(profilePath) //nolint:gosec // G304: the user names the scope's files
	if err != nil {
		return err
	}
	demoJSON, err := os.ReadFile(demoPath) //nolint:gosec // G304: the user names the scope's files
	if err != nil {
		return err
	}
	if err := simdemo.BuildSite(context.Background(), profileYAML, demoJSON, out); err != nil {
		return fmt.Errorf("%s: %w", demoPath, err)
	}
	return nil
}
