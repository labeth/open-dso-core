// Package agent is the on-device update supervisor of the open scope
// firmwares: the permanent holder of what the boot chain handed it (on the
// SDS1000CML+ the inherited bus descriptors), the owner of the hardware
// watchdog after takeover, the launcher and supervisor of the app (A/B slots
// and health rollback), and the remote-orchestration endpoint (NATS and a
// local TCP JSON-RPC). What only one scope does - its takeover of the vendor
// app, its device nodes, its fingerprint - it asks of its Board.
//
// Modes:
//   - coexist (default): the vendor app runs the instrument; the agent only
//     provides remote access and never touches the instrument's bus.
//   - taken over: the vendor app has been stopped at an idle landing and
//     killed; the agent pets the watchdog and supervises the app slots.
//
// ENGMODEL-OWNER-UNIT: FU-OTA-AGENT
package agent

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/supervisor/config"
	"github.com/labeth/open-dso-core/supervisor/slots"
	"github.com/labeth/open-dso-core/supervisor/sysinfo"
	"github.com/labeth/open-dso-core/supervisor/watchdog"
)

const (
	appPidFile   = "ota-app.pid"
	agentPidFile = "ota-agent.pid"
)

// Agent is the supervisor process: one per device, created by New and
// driven by Run until Stop.
// TRLC-LINKS: REQ-SDS-122
type Agent struct {
	cfg     *config.Config
	board   Board
	version string
	st      *stateFile
	store   *slots.Store
	wd      *watchdog.Keeper
	log     *log.Logger

	started time.Time
	hbSeq   atomic.Int64

	// slot is AgentSlot's answer, worked out once: the running binary does
	// not change, and an update writes the other slot.
	slotOnce sync.Once
	slot     string

	// supervisor control
	ctl     chan ctlMsg
	stopped chan struct{}

	// live app state (guarded)
	appMu         sync.Mutex
	app           appState
	paused        bool
	confirmedRun  string // the slot this run found confirmed (supervisor only)
	confirmFailed string // the slot whose confirm write last failed, reported once (supervisor only)

	// upload staging sessions
	upMu sync.Mutex
	ups  map[string]*uploadSession

	// event sink (wired by the NATS link; nil-safe)
	eventMu sync.Mutex
	eventFn func(kind string, detail map[string]any)

	// takeover serialization
	tkMu sync.Mutex

	// TCP control listener bound address (set once serveTCP is listening;
	// observable so tests and diagnostics can find an OTA_LISTEN=:0 port)
	tcpMu   sync.Mutex
	tcpAddr string
}

// TRLC-LINKS: REQ-SDS-028
type appState struct {
	Running   bool   `json:"running"`
	Adopted   bool   `json:"adopted"` // orphan from a previous agent generation
	PID       int    `json:"pid,omitempty"`
	Slot      string `json:"slot,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Fails     int    `json:"fails"`
	LastExit  string `json:"last_exit,omitempty"`
	Health    any    `json:"health,omitempty"`
}

// ctlOp is a control request to the supervisor.
// TRLC-LINKS: REQ-SDS-117
type ctlOp string

// The supervisor's control requests: stop pauses it (terminating the app),
// start resumes it, restart relaunches the app.
const (
	opStart   ctlOp = "start"
	opStop    ctlOp = "stop"
	opRestart ctlOp = "restart"
)

// TRLC-LINKS: REQ-SDS-117
type ctlMsg struct {
	op    ctlOp
	reply chan error
}

// New builds the agent from its configuration and board: loads the persisted
// state; the app slots are the board's SlotsDir. version is the build's
// identity line, reported in status, discovery and the online event. It
// starts nothing; Run does.
// TRLC-LINKS: REQ-SDS-122
func New(cfg *config.Config, board Board, version string) *Agent {
	a := &Agent{
		cfg:     cfg,
		board:   board,
		version: version,
		st:      loadState(cfg.StatePath()),
		store:   slots.New(board.SlotsDir()),
		wd:      newKeeper(board),
		log:     log.New(os.Stdout, "[agent] ", log.LstdFlags),
		started: time.Now(),
		ctl:     make(chan ctlMsg, 4),
		stopped: make(chan struct{}),
		ups:     map[string]*uploadSession{},
	}
	if at, ok := board.(Attacher); ok {
		at.Attach(a)
	}
	return a
}

// Run starts everything and blocks until Stop.
// TRLC-LINKS: REQ-SDS-122
func (a *Agent) Run() error {
	if err := a.store.Init(); err != nil {
		a.log.Printf("slot store init: %v (continuing)", err)
	}
	_ = os.WriteFile(a.pidPath(agentPidFile), fmt.Appendf(nil, "%d\n", os.Getpid()), 0o600)

	a.log.Printf("open-sds OTA agent %s device=%s slot=%s", a.version, a.cfg.DeviceID, a.AgentSlot())
	if fds := a.board.BootFDs(); len(fds) > 0 {
		a.log.Printf("inherited fds: %v", fds)
	}
	st := a.st.get()
	a.log.Printf("state: taken_over=%v auto_takeover=%v", st.TakenOver, st.AutoTakeover)

	// Already-taken-over restart (agent update / crash respawn): the factory
	// app is long dead; nothing pets the watchdog until we do. Acquire NOW.
	switch {
	case st.TakenOver:
		go a.keepWatchdog()
	case st.AutoTakeover || a.cfg.AutoTakeover:
		// Default is COEXIST: the factory app keeps driving the instrument and
		// the agent only provides remote access + probe. Auto-takeover happens
		// only when explicitly armed (OTA_AUTO_TAKEOVER or a persisted flag).
		go func() {
			// The factory app is already up and settled by the time the firmware
			// runs startup.sh (it starts long before), so take over immediately.
			// OTA_TAKEOVER_DELAY can still add a settle wait for an odd unit.
			if a.cfg.TakeoverDelay > 0 {
				a.log.Printf("auto-takeover armed: settling %s", a.cfg.TakeoverDelay)
				time.Sleep(a.cfg.TakeoverDelay)
			} else {
				a.log.Printf("auto-takeover armed: taking over now")
			}
			res := a.Takeover(TakeoverOpts{})
			a.log.Printf("auto-takeover: ok=%v %s", res.OK, res.Summary())
		}()
	default:
		a.log.Printf("coexist mode: factory app owns the instrument; run `otactl takeover` when ready")
	}

	go a.superviseLoop()
	go a.boardHeartbeat()

	if a.cfg.TCPListen != "" {
		go a.serveTCP()
	}
	if a.cfg.NATSURL != "" {
		go a.runNATS()
	} else {
		a.log.Printf("OTA_NATS not set — NATS link disabled (local TCP control listener only)")
	}

	<-a.stopped
	return nil
}

// Stop performs the clean-shutdown path: disarm the watchdog with the magic
// byte so the driver doesn't reset while the respawn loop restarts us. The
// app (if any) is left running — the next agent generation adopts it.
// TRLC-LINKS: REQ-SDS-122
func (a *Agent) Stop() {
	select {
	case <-a.stopped:
		return
	default:
	}
	a.wd.Disarm()
	close(a.stopped)
}

// newKeeper is the keeper of the board's watchdog, if it has one.
// TRLC-LINKS: REQ-SDS-086
func newKeeper(b hal.SupervisorBoard) *watchdog.Keeper {
	if wd, ok := b.Watchdog(); ok {
		return watchdog.NewKeeper(wd)
	}
	return watchdog.NewKeeper(nil)
}

// TakenOver reports the persisted taken_over state.
// TRLC-LINKS: REQ-SDS-104
func (a *Agent) TakenOver() bool { return a.st.get().TakenOver }

// Watchdog reports the watchdog keeper's state.
// TRLC-LINKS: REQ-SDS-086
func (a *Agent) Watchdog() watchdog.Status { return a.wd.Status() }

// DisarmWatchdog stops petting and disarms the watchdog.
// TRLC-LINKS: REQ-SDS-086
func (a *Agent) DisarmWatchdog() { a.wd.Disarm() }

// boardHeartbeat calls the board's Heartbeat every heartbeat interval, for a
// board whose platform expects one; a failure is logged when it changes.
// TRLC-LINKS: REQ-SDS-122
func (a *Agent) boardHeartbeat() {
	t := time.NewTicker(a.cfg.HBInterval)
	defer t.Stop()
	last := ""
	for {
		select {
		case <-a.stopped:
			return
		case <-t.C:
			msg := ""
			if err := a.board.Heartbeat(); err != nil {
				msg = err.Error()
			}
			if msg != last {
				a.log.Printf("board heartbeat: %q", msg)
				last = msg
			}
		}
	}
}

// TRLC-LINKS: REQ-SDS-122
func (a *Agent) pidPath(name string) string {
	return filepath.Join(a.cfg.HealthDir, name)
}

// AgentSlot reports which A/B agent binary this process is (or "?" off-slot).
// TRLC-LINKS: REQ-SDS-114
func (a *Agent) AgentSlot() string {
	a.slotOnce.Do(func() { a.slot = a.findSlot() })
	return a.slot
}

// TRLC-LINKS: REQ-SDS-114
func (a *Agent) findSlot() string {
	exe, err := os.Executable()
	if err != nil {
		return "?"
	}
	exe, _ = filepath.EvalSymlinks(exe)
	for _, s := range [...]struct{ slot, path string }{{"A", a.cfg.AgentA}, {"B", a.cfg.AgentB}} {
		if rp, err := filepath.EvalSymlinks(s.path); err == nil && rp == exe {
			return s.slot
		}
	}
	return "?"
}

// TRLC-LINKS: REQ-SDS-123
func (a *Agent) event(kind string, detail map[string]any) {
	a.log.Printf("event %s: %v", kind, detail)
	a.eventMu.Lock()
	fn := a.eventFn
	a.eventMu.Unlock()
	if fn != nil {
		fn(kind, detail)
	}
}

// TRLC-LINKS: REQ-SDS-123
func (a *Agent) setEventFn(fn func(string, map[string]any)) {
	a.eventMu.Lock()
	a.eventFn = fn
	a.eventMu.Unlock()
}

// keepWatchdog runs acquireWatchdogForever and logs why it gave up (only an
// agent stop ends it).
// TRLC-LINKS: REQ-SDS-122
func (a *Agent) keepWatchdog() {
	if err := a.acquireWatchdogForever(); err != nil {
		a.log.Printf("watchdog: %v", err)
	}
}

// acquireWatchdogForever keeps trying until the watchdog is ours; used after
// takeover (or on restart in the taken-over state). Failure to acquire while
// the vendor app is dead means a warm reset in ~60 s on a scope with a
// watchdog, so never give up; a board without one has nothing to acquire.
// TRLC-LINKS: REQ-SDS-122
func (a *Agent) acquireWatchdogForever() error {
	for i := 0; ; i++ {
		err := a.wd.Acquire(15*time.Second, a.cfg.WdPet)
		if errors.Is(err, watchdog.ErrNone) {
			return nil
		}
		if err == nil {
			a.log.Printf("watchdog acquired, petting every %s", a.cfg.WdPet)
			return nil
		}
		if i%4 == 0 { // don't spam the log
			a.log.Printf("watchdog acquire failed (attempt %d): %v — retrying", i+1, err)
		}
		select {
		case <-a.stopped:
			return err
		case <-time.After(5 * time.Second):
		}
	}
}

// TRLC-LINKS: REQ-SDS-114
type statusDoc struct {
	Device       string          `json:"device"`
	Version      string          `json:"version"`
	AgentSlot    string          `json:"agent_slot"`
	UptimeS      int64           `json:"uptime_s"`
	TakenOver    bool            `json:"taken_over"`
	AutoTakeover bool            `json:"auto_takeover"`
	Paused       bool            `json:"paused"`
	App          appState        `json:"app"`
	Slots        slots.Status    `json:"slots"`
	Watchdog     watchdog.Status `json:"watchdog"`
	FDs          map[string]int  `json:"inherited_fds"`
	Sys          sysinfo.Info    `json:"sys"`
	TimeUnix     int64           `json:"time_unix"`
}

// TRLC-LINKS: REQ-SDS-114
func (a *Agent) status() statusDoc {
	st := a.st.get()
	a.appMu.Lock()
	app := a.app
	paused := a.paused
	a.appMu.Unlock()
	return statusDoc{
		Device:       a.cfg.DeviceID,
		Version:      a.version,
		AgentSlot:    a.AgentSlot(),
		UptimeS:      int64(time.Since(a.started).Seconds()),
		TakenOver:    st.TakenOver,
		AutoTakeover: st.AutoTakeover,
		Paused:       paused,
		App:          app,
		Slots:        a.store.Status(),
		Watchdog:     a.wd.Status(),
		FDs:          a.board.BootFDs(),
		Sys:          sysinfo.Collect(a.cfg.OTADir),
		TimeUnix:     time.Now().Unix(),
	}
}
