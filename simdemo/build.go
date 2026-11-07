package simdemo

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/labeth/open-dso-core/design"
)

// WasmFile is the demo's module in a built site.
const WasmFile = "dsodemo.wasm"

// BuildSite writes the browser demo of a scope's profile and demo file to
// out as a static site: the bench page and its workers, the shared
// stylesheets, the scope's data, Go's wasm_exec.js and the demo's module
// (cmd/dsodemo-wasm), built with the go command from this checkout. The
// scope's data must make a board before anything is written.
// TRLC-LINKS: REQ-DSO-161, REQ-DSO-295
func BuildSite(ctx context.Context, profileYAML, demoJSON []byte, out string) error {
	d, err := ParseDemo(demoJSON)
	if err != nil {
		return err
	}
	if _, err := Board(profileYAML, d, Options{}); err != nil {
		return err
	}
	for _, dir := range []string{out, filepath.Join(out, "scope"), filepath.Join(out, "design")} {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: a site's directories are public
			return err
		}
	}
	put := func(name string, b []byte) error { return os.WriteFile(filepath.Join(out, name), b, 0o644) } //nolint:gosec // G306: a site's files are public
	for name, b := range map[string][]byte{
		"scope/profile.yaml": profileYAML, "scope/demo.json": demoJSON, "mode.json": Mode{Mode: "wasm", UI: "ui/"}.JSON(),
	} {
		if err := put(name, b); err != nil {
			return err
		}
	}
	if err := copyFS(Site(), out); err != nil {
		return err
	}
	if err := copyFS(design.CSS, filepath.Join(out, "design")); err != nil {
		return err
	}
	goroot, err := exec.CommandContext(ctx, "go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env GOROOT: %w", err)
	}
	glue, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"))
	if err != nil {
		return err
	}
	if err := put("wasm_exec.js", glue); err != nil {
		return err
	}
	return buildWasm(ctx, filepath.Join(out, WasmFile))
}

// copyFS writes every file of src but its Go sources into dir.
// TRLC-LINKS: REQ-DSO-161
func copyFS(src fs.FS, dir string) error {
	return fs.WalkDir(src, ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || strings.HasSuffix(p, ".go") {
			return err
		}
		b, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // G301: a site's directories are public
			return err
		}
		return os.WriteFile(dst, b, 0o644) //nolint:gosec // G306: a site's files are public
	})
}

// buildWasm builds the demo's module, stripped, into dst, from this
// checkout of the core.
// TRLC-LINKS: REQ-DSO-295
func buildWasm(ctx context.Context, dst string) error {
	dst, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags=-s -w", "-o", dst, "./cmd/dsodemo-wasm") //nolint:gosec // G204: the output path is the caller's
	cmd.Dir = coreDir()
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// coreDir is the core checkout this package was built from ("" for the
// working directory when the build kept no source paths).
// TRLC-LINKS: REQ-DSO-295
func coreDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(file) {
		return ""
	}
	return filepath.Dir(filepath.Dir(file))
}
