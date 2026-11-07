// ENGMODEL-OWNER-UNIT: FU-OTA-AGENT

package agent

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/supervisor/config"
	"github.com/labeth/open-dso-core/supervisor/fdinherit"
)

// fileWatchdog is a hal.Watchdog over a plain temp file: open and write
// behave as on a device node, and no test arms the host's real watchdog.
// TRLC-LINKS: REQ-SDS-086
type fileWatchdog struct {
	path string
	f    *os.File
}

// TRLC-LINKS: REQ-SDS-086
func (w *fileWatchdog) Open() error {
	f, err := os.OpenFile(w.path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	w.f = f
	return nil
}

// TRLC-LINKS: REQ-SDS-086
func (w *fileWatchdog) Pet() error {
	_, err := w.f.Write([]byte{0x01})
	return err
}

// TRLC-LINKS: REQ-SDS-086
func (w *fileWatchdog) Close() error {
	_, _ = w.f.Write([]byte{'V'})
	return w.f.Close()
}

// fakeBoard is a board with no vendor app: no boot descriptors, no vendor
// holders unless a test sets them, and a temp-file watchdog.
// TRLC-LINKS: REQ-SDS-027, REQ-SDS-122
type fakeBoard struct {
	slots string
	wd    *fileWatchdog // nil: no watchdog

	mu       sync.Mutex
	holders  []fdinherit.Holder
	land     func(res *TakeoverResult, opts TakeoverOpts) bool
	reclaims int
	killed   int
}

// TRLC-LINKS: REQ-SDS-086
func (f *fakeBoard) Watchdog() (hal.Watchdog, bool) {
	if f.wd == nil {
		return nil, false
	}
	return f.wd, true
}

// TRLC-LINKS: REQ-SDS-027
func (f *fakeBoard) Takeover(ctx context.Context) error {
	res := ResultOf(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	res.Killed = len(f.holders)
	f.killed += len(f.holders)
	f.holders = nil
	res.Step("killed %d factory processes", res.Killed)
	return nil
}

// TRLC-LINKS: REQ-SDS-028
func (f *fakeBoard) LaunchEnv() ([]string, []*os.File) { return []string{"SCOPE_FAKE=1"}, nil }

// TRLC-LINKS: REQ-SDS-028
func (f *fakeBoard) Alive(pid int) bool { return fdinherit.Alive(pid) }

// TRLC-LINKS: REQ-SDS-028
func (f *fakeBoard) SlotsDir() string { return f.slots }

// TRLC-LINKS: REQ-SDS-121
func (f *fakeBoard) Reboot(string) error { return errors.New("fake board: no reboot") }

// TRLC-LINKS: REQ-SDS-122
func (f *fakeBoard) Heartbeat() error { return nil }

// TRLC-LINKS: REQ-SDS-114
func (f *fakeBoard) BootFDs() map[string]int { return map[string]int{"bus": -1} }

// TRLC-LINKS: REQ-SDS-027
func (f *fakeBoard) VendorHolders() []fdinherit.Holder {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fdinherit.Holder(nil), f.holders...)
}

// TRLC-LINKS: REQ-SDS-027
func (f *fakeBoard) Land(res *TakeoverResult, opts TakeoverOpts) bool {
	if f.land != nil {
		return f.land(res, opts)
	}
	res.Candidates = f.VendorHolders()
	if opts.DryRun {
		res.OK = true
		res.Step("dry-run: stopping here (no STOP sent, nothing killed)")
		return false
	}
	return true
}

// TRLC-LINKS: REQ-SDS-027
func (f *fakeBoard) Reclaim(res *TakeoverResult) {
	f.mu.Lock()
	f.reclaims++
	f.holders = nil
	f.mu.Unlock()
	res.Step("reclaimed")
}

// TRLC-LINKS: REQ-SDS-114
func (f *fakeBoard) Probe(deep, takenOver bool) any {
	return map[string]bool{"deep": deep, "taken_over": takenOver}
}

// newFakeBoard is a fakeBoard on the configuration's slot root, with a
// temp-file watchdog in dir.
// TRLC-LINKS: REQ-SDS-122
func newFakeBoard(cfg *config.Config, dir string) *fakeBoard {
	wd := dir + "/watchdog"
	if err := os.WriteFile(wd, nil, 0o644); err != nil {
		panic(err)
	}
	return &fakeBoard{slots: cfg.SlotRoot, wd: &fileWatchdog{path: wd}}
}
