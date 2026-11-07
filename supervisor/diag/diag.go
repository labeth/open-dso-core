// ENGMODEL-OWNER-UNIT: FU-DSO-SUPERVISOR-DIAG

// Package diag is the framework of a diagnostics stick's collector: booted
// by the vendor firmware on a scope, it leaves one archive of what that board
// is and does, for a port of the open firmware to its population. The stages
// run in order, each only when the checks before it pass: the passive stage
// only reads; the active stage drives the vendor app and puts its settings
// back; the takeover stage stops the vendor app and examines the board with
// the open firmware's own images and app. The framework holds the run (modes,
// flags, the status file on the stick, the summary, the archive, the DONE
// screen) and the collectors any Linux scope shares; a Board holds what the
// stages do on one scope.
package diag

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The modes a stick runs in: MODE on the stick names one. Auto (the
// default) runs the passive stage, then the active stage when the gate finds
// the scope is one it understands, then the takeover stage when its own gate
// passes; active runs the active stage after passive without the gate, and
// stops there; passive runs neither.
const (
	ModeAuto    = "auto"
	ModePassive = "passive"
	ModeActive  = "active"
)

// archiveMax caps the archive's stored bytes (a register trace is the big
// member).
const archiveMax = 512 << 20

// Board is what the stages do on one scope.
type Board interface {
	// Version names the build on the summary's first line.
	Version() string
	// Child runs one of the board's child-process subcommands (a tracer, a
	// register reader in a process of its own); ok is false for a name that
	// is none of its own.
	Child(name string, args []string, stderr io.Writer) (code int, ok bool)
	// Passive is the board's part of the passive stage, after the system
	// collection: what only reads. It returns false when no later stage can
	// run (the vendor app cannot be reached).
	Passive(c *Collection, a *Archive) bool
	// ActiveGate is the checks the active stage needs in auto mode.
	ActiveGate(c *Collection) []Check
	// Active is the active stage.
	Active(c *Collection, a *Archive)
	// TakeoverGate is the checks the takeover stage needs (auto mode, after
	// the active stage).
	TakeoverGate(c *Collection) []Check
	// Takeover is the takeover stage; it sets c.TookOver once the vendor app
	// is stopped.
	Takeover(c *Collection, a *Archive)
	// Close ends what the stages opened (the link to the vendor app).
	Close()
	// ScreenOn lights the screen the DONE banner is drawn on.
	ScreenOn() error
}

// Run is the program: no subcommand collects; any other is one of the
// board's own child processes. It returns the exit code.
// TRLC-LINKS: REQ-DSO-284, REQ-DSO-285, REQ-DSO-286
func Run(args []string, stderr io.Writer, board Board) int {
	if len(args) > 0 {
		if code, ok := board.Child(args[0], args[1:], stderr); ok {
			return code
		}
	}
	fs := flag.NewFlagSet("sdsdiag", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mode := fs.String("mode", "", "auto (every stage, each gated), active (passive and active) or passive (empty: the MODE file next to the program; auto if none)")
	out := fs.String("out", "", "where the archive goes (empty: the stick's root, above the program's directory)")
	wait := fs.Duration("wait", 150*time.Second, "how long to wait for the factory app's SCPI server")
	dwell := fs.Duration("dwell", 700*time.Millisecond, "how long each sweep step is held (active)")
	steps := fs.Int("steps", 0, "run only the first steps of the sweep (active; 0: all)")
	limit := fs.Duration("limit", 30*time.Minute, "the longest the active sweep runs before it puts the settings back")
	pid := fs.Int("pid", 0, "trace this process instead of the factory app (active)")
	hold := fs.Duration("hold", 30*time.Minute, "how long the DONE screen is kept over the factory app's")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	self, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "sdsdiag:", err)
		return 1
	}
	dir := filepath.Dir(self)
	if *mode == "" {
		*mode = strings.TrimSpace(ReadStr(filepath.Join(dir, "MODE")))
	}
	if *mode != ModeActive && *mode != ModePassive {
		*mode = ModeAuto
	}
	if *out == "" {
		*out = filepath.Dir(dir)
	}
	c := &Collection{
		Self: self, Mode: *mode, OutDir: *out, Wait: *wait, Dwell: *dwell, Steps: *steps, Limit: *limit, PID: *pid, Hold: *hold,
		status: filepath.Join(*out, "sds-diag-status.txt"), board: board,
	}
	if err := c.run(); err != nil {
		c.Say("FAILED: %v", err)
		_, _ = fmt.Fprintln(stderr, "sdsdiag:", err)
		c.doneScreen(false, []string{err.Error(), "see sds-diag-status.txt on the stick"})
		return 1
	}
	return 0
}

// Collection is one run of the collector.
type Collection struct {
	Self   string // this program
	Mode   string
	OutDir string // where the archive goes

	Wait  time.Duration // how long to wait for the vendor app's control service
	Dwell time.Duration // how long each active step is held
	Steps int           // the active stage's first steps only (0: all)
	Limit time.Duration // the longest the active stage runs
	PID   int           // the process to trace (0: the vendor app)
	Hold  time.Duration // how long the DONE screen is kept over the vendor app's

	// TookOver is set by the takeover stage once the vendor app is stopped:
	// the DONE screen is then drawn once, with nobody drawing over it.
	TookOver bool

	status  string
	board   Board
	summary []string
}

// Say writes a progress line to the status file on the stick: the person who
// booted the stick sees what it is doing when they take it out.
// TRLC-LINKS: REQ-DSO-286
func (c *Collection) Say(format string, args ...any) {
	line := fmt.Sprintf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
	f, err := os.OpenFile(c.status, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec // G302,G304: a text file the owner reads on the stick
	if err != nil {
		return
	}
	_, _ = f.WriteString(line)
	_ = f.Close()
}

// Note adds a line to the summary.
// TRLC-LINKS: REQ-DSO-286
func (c *Collection) Note(format string, args ...any) {
	c.summary = append(c.summary, fmt.Sprintf(format, args...))
}

// run collects into a new archive on the stick.
// TRLC-LINKS: REQ-DSO-284, REQ-DSO-285, REQ-DSO-286
func (c *Collection) run() error {
	name := archiveName(c.OutDir, c.Mode)
	path := filepath.Join(c.OutDir, name)
	c.Say("collecting (%s) into %s - leave the scope on until DONE", c.Mode, name)
	f, err := os.Create(path) //nolint:gosec // G304: the archive on the stick
	if err != nil {
		return err
	}
	a := NewArchive(f, archiveMax)
	c.Note("sdsdiag %s, mode %s", c.board.Version(), c.Mode)
	c.Note("kernel: %s", strings.TrimSpace(ReadStr("/proc/version")))
	c.Note("cpu: %s", CPULine())
	c.Note("mac: %s", strings.TrimSpace(ReadStr("/sys/class/net/eth0/address")))

	c.Say("system")
	CollectSystem(a)
	c.stages(a)
	errs := a.Errors()
	c.Note("collector errors: %d (errors.txt)", len(errs))
	a.AddText("summary.txt", strings.Join(c.summary, "\n")+"\n")
	if err := a.Close(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(c.OutDir, strings.TrimSuffix(name, ".tar.gz")+".txt"), []byte(strings.Join(c.summary, "\n")+"\n"), 0o644) //nolint:gosec // G306: the summary the owner reads on the stick
	c.Say("DONE: send %s to the project (it holds this scope's firmware files: send it privately)", name)
	c.doneScreen(true, []string{name, "switch the scope off", "and take the stick out"})
	return nil
}

// stages runs the board's passive stage and, when the mode and the gates
// allow it, the active and takeover stages.
// TRLC-LINKS: REQ-DSO-284, REQ-DSO-285, REQ-DSO-287, REQ-DSO-288
func (c *Collection) stages(a *Archive) {
	defer c.board.Close()
	if !c.board.Passive(c, a) {
		return
	}
	active := c.Mode == ModeActive
	if c.Mode == ModeAuto {
		checks := c.board.ActiveGate(c)
		a.AddJSON("gate.json", checks)
		ok, why := Passed(checks)
		if ok {
			c.Note("gate: every check passed - the active stage runs")
		} else {
			c.Note("gate: the active stage does not run (%s)", why)
			c.Say("passive only: %s", why)
		}
		active = ok
	}
	if !active {
		return
	}
	c.board.Active(c, a)
	if c.Mode != ModeAuto {
		return
	}
	checks := c.board.TakeoverGate(c)
	a.AddJSON("takeover/gate.json", checks)
	if ok, why := Passed(checks); !ok {
		c.Note("takeover gate: the takeover stage does not run (%s)", why)
		c.Say("no takeover: %s", why)
		return
	}
	c.Note("takeover gate: every check passed - the takeover stage runs")
	c.board.Takeover(c, a)
}

// ProcState is the state letter of /proc/pid/stat ("R", "S", "T", ...), or
// "" when the process is gone.
// TRLC-LINKS: REQ-DSO-285
func ProcState(pid int) string {
	st := ReadStr(fmt.Sprintf("/proc/%d/stat", pid))
	i := strings.LastIndexByte(st, ')') // the name in parentheses can hold spaces
	if i < 0 || i+2 >= len(st) {
		return ""
	}
	return st[i+2 : i+3]
}

// archiveName is the next free archive name in dir for this scope and mode:
// the MAC (the clock is not set on these scopes) and a sequence number.
// TRLC-LINKS: REQ-DSO-286
func archiveName(dir, mode string) string {
	mac := strings.ReplaceAll(strings.TrimSpace(ReadStr("/sys/class/net/eth0/address")), ":", "")
	if mac == "" {
		mac = "nomac"
	}
	for n := 1; ; n++ {
		name := fmt.Sprintf("sds-diag-%s-%s-%d.tar.gz", mac, mode, n)
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			return name
		}
	}
}

// CPULine is the processor and hardware lines of /proc/cpuinfo.
// TRLC-LINKS: REQ-DSO-284
func CPULine() string {
	var parts []string
	for l := range strings.SplitSeq(ReadStr("/proc/cpuinfo"), "\n") {
		if strings.HasPrefix(l, "Processor") || strings.HasPrefix(l, "Hardware") || strings.HasPrefix(l, "model name") {
			parts = append(parts, strings.TrimSpace(l[strings.Index(l, ":")+1:]))
		}
	}
	return strings.Join(parts, ", ") + " (" + runtime.GOARCH + ")"
}

// ErrText is err's text in parentheses, or "" for nil.
// TRLC-LINKS: REQ-DSO-284
func ErrText(err error) string {
	if err == nil {
		return ""
	}
	return "(" + err.Error() + ")"
}
