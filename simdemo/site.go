package simdemo

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/labeth/open-dso-core/design"
)

// siteFiles are the bench page and its workers: index.html, demo.js,
// demo.css, sw.js (the service worker) and engine.js (the engine's
// worker).
//
//go:embed site
var siteFiles embed.FS

// Site is the bench page's files, at the top level.
// TRLC-LINKS: REQ-DSO-161
func Site() fs.FS {
	sub, err := fs.Sub(siteFiles, "site")
	if err != nil {
		panic(err) // the embedded directory is there by construction
	}
	return sub
}

// Mode is mode.json: how the bench page reaches the firmware ("wasm": in
// the tab; "native": the server that serves the page) and where the web UI
// is.
type Mode struct {
	Mode string `json:"mode"`
	UI   string `json:"ui"`
}

// JSON is m as mode.json.
// TRLC-LINKS: REQ-DSO-161
func (m Mode) JSON() []byte {
	b, _ := json.Marshal(m) // two strings always marshal
	return b
}

// SiteHandler serves the bench page under its mount point (the native
// host's /demo/): the page's files, the shared stylesheets under design/,
// the scope's data under scope/ and mode.json.
// TRLC-LINKS: REQ-DSO-161
func SiteHandler(prefix string, mode Mode, profileYAML, demoJSON []byte) http.Handler {
	site := http.FileServerFS(Site())
	return http.StripPrefix(strings.TrimSuffix(prefix, "/"), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		p := path.Clean("/" + r.URL.Path)
		switch {
		case p == "/mode.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(mode.JSON())
		case p == "/scope/profile.yaml":
			w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
			_, _ = w.Write(profileYAML)
		case p == "/scope/demo.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(demoJSON)
		case strings.HasPrefix(p, "/design/"):
			b, err := fs.ReadFile(design.CSS, strings.TrimPrefix(p, "/design/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			_, _ = w.Write(b) //nolint:gosec // G705: an embedded stylesheet, named by the path but never echoed from it
		default:
			site.ServeHTTP(w, r)
		}
	}))
}
