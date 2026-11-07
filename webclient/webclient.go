// Package webclient is the web UI every scope serves: the pages, the
// classic-script JS modules, the phone remote's manifest, icons and
// keep-awake videos, and, for tests only, the node harnesses (*.mjs, *.cjs)
// and the operator e2e harness (e2e/). The stylesheets are in design.
package webclient

import (
	"embed"
	"path/filepath"
	"runtime"
)

// FS holds the served client files at the top level.
//
//go:embed ui.html remote.html *.js remote.webmanifest remote-*.png remote-awake.mp4 remote-awake.webm
var FS embed.FS

// Dir is this package's directory on disk, where tests run the node
// harnesses against the JS modules. It is only meaningful in a build from
// source (tests), not in a deployed binary.
// TRLC-LINKS: REQ-DSO-161
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Dir(file)
}
