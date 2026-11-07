// Command otactl is the host-side controller for the open scope firmwares'
// update agents (see package otactl). A scope's ota module builds its own
// otactl with its device's staging directory as the -stage default; this
// one has none, so uploads name -stage.
//
// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-OTACTL
package main

import (
	"os"

	"github.com/labeth/open-dso-core/supervisor/otactl"
)

// version is stamped at link time (-ldflags "-X main.version=...").
var version = "dev"

// TRLC-LINKS: REQ-DSO-109, REQ-DSO-175
func main() {
	os.Exit(otactl.Run(os.Args[1:], os.Stdout, os.Stderr, otactl.Options{Version: version, StageDoc: "none: pass -stage"}))
}
