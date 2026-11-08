// ENGMODEL-OWNER-UNIT: FU-DSO-BOOTTARGET

// Package boottarget is the owner's choice of what the next power-up runs:
// this firmware or the vendor firmware. A scope that starts from a vendor
// launch hook keeps a disable file in its firmware's directory; while the
// file exists the hook exits at once and leaves the unit to the vendor
// firmware. Setting the vendor target writes the file, the open target
// removes it, both through the board's write guard; nothing is uninstalled.
// The board names the directory (Switch); the SYS sheet, SCPI
// (:SYSTem:BOOT) and the web page (/api/boot) set it through one
// hal.BootTarget (Extension).
package boottarget

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labeth/open-dso-core/fsguard"
	"github.com/labeth/open-dso-core/hal"
)

// DisableName is the disable file's name in the firmware's directory.
const DisableName = "disable"

// Next returns the target of the next power-up for the firmware directory
// dir: hal.BootVendor while the disable file exists.
// TRLC-LINKS: REQ-DSO-297
func Next(dir string) string {
	if _, err := os.Lstat(filepath.Join(dir, DisableName)); err == nil {
		return hal.BootVendor
	}
	return hal.BootOpen
}

// Set makes target the next power-up's: hal.BootVendor writes the disable
// file with reason as its content, hal.BootOpen removes it, both through g.
// TRLC-LINKS: REQ-DSO-297
func Set(g *fsguard.Guard, dir, target, reason string) error {
	p := filepath.Join(dir, DisableName)
	switch target {
	case hal.BootVendor:
		if err := g.MkdirAll(dir); err != nil {
			return err
		}
		if reason == "" {
			reason = "asked for the vendor firmware"
		}
		return g.WriteFile(p, []byte(strings.TrimSpace(reason)+"\n"), 0o644)
	case hal.BootOpen:
		return g.Remove(p)
	}
	return fmt.Errorf("boot target %q: %w", target, ErrTarget)
}

// ErrTarget is a target other than open and vendor.
var ErrTarget = errors.New("the boot targets are open and vendor")

// Parse reads a target as SCPI, the web page and a supervisor's command
// line spell it: OPEN/VENDOR in any case, or the SCPI short form VEND.
// TRLC-LINKS: REQ-DSO-297
func Parse(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "open":
		return hal.BootOpen, nil
	case "vendor", "vend":
		return hal.BootVendor, nil
	}
	return "", fmt.Errorf("boot target %q: %w", s, ErrTarget)
}

// Switch is a board's boot target: the disable file in Dir, written through
// Guard.
type Switch struct {
	Guard *fsguard.Guard
	Dir   string
}

var _ hal.BootTarget = Switch{}

// Next is the next power-up's target.
// TRLC-LINKS: REQ-DSO-297
func (s Switch) Next() string { return Next(s.Dir) }

// Set makes target the next power-up's.
// TRLC-LINKS: REQ-DSO-297
func (s Switch) Set(target, why string) error { return Set(s.Guard, s.Dir, target, why) }

// Feature names the extension in the capabilities.
const Feature = "boot-target"

// Extension serves t as :SYSTem:BOOT OPEN|VENDOR (query :SYSTem:BOOT?) and
// GET/POST /api/boot ({"next": "open"|"vendor"}; POST takes ?next= or the
// body). It touches no hardware, so its handlers may run on the transports'
// goroutines.
// TRLC-LINKS: REQ-DSO-297
func Extension(t hal.BootTarget) hal.Extension {
	set := func(arg, via string) error {
		target, err := Parse(arg)
		if err != nil {
			return hal.Refuse("boot target", err.Error())
		}
		return t.Set(target, "asked through "+via)
	}
	return hal.Extension{
		Feature: Feature,
		SCPI: []hal.SCPIHeader{{
			Header:  ":SYSTem:BOOT",
			Command: func(args string) error { return set(args, "SCPI") },
			Query:   func(string) ([]byte, error) { return []byte(strings.ToUpper(t.Next())), nil },
		}},
		Routes: []hal.Route{
			{Pattern: "GET /api/boot", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, t.Next())
			})},
			{Pattern: "POST /api/boot", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				arg := r.URL.Query().Get("next")
				if arg == "" {
					b, _ := io.ReadAll(io.LimitReader(r.Body, 64)) // a short body; a read error leaves it empty, which Parse refuses
					arg = string(b)
				}
				if err := set(arg, "the web page"); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				writeJSON(w, t.Next())
			})},
		},
	}
}

// TRLC-LINKS: REQ-DSO-297
func writeJSON(w http.ResponseWriter, next string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"next": next}) // a failed write is the client's gone connection
}
