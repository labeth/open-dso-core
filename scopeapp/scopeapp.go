// Package scopeapp is a scope's application: one composition every scope's
// main hands its board to (scopeapp.Run), so a main is the board plus one
// call. It brings the board up, builds the instrument state (the core's
// Model over the HAL, or the scope's own owner while that is not yet HAL
// terms), runs the owner loop and its fan-out, the UI model over the front
// panel, the display loop, SCPI on the LAN transports with the scope's
// dialect, the setup's persistence and the web server, writes the
// supervisor's health token, and on the end of its context saves what is
// pending and stops. What only one scope has enters as an Option, named for
// what it is; there are no modes (ADR-ONE-APP).
// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEAPP
package scopeapp

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/labeth/open-dso-core/engine"
	"github.com/labeth/open-dso-core/engine/fanout"
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/render"
	"github.com/labeth/open-dso-core/sample"
	"github.com/labeth/open-dso-core/scopeweb"
	scpihost "github.com/labeth/open-dso-core/scpi/host"
	"github.com/labeth/open-dso-core/settings"
	"github.com/labeth/open-dso-core/uimodel/panel"
	"github.com/labeth/open-dso-core/webserver"
	"github.com/labeth/open-dso-core/webserver/wire"
)

// Options are what a scope adds to the composition. Every field may be
// left zero for a scope whose board is all HAL terms.
type Options struct {
	Logf func(format string, a ...any)
	// Mark logs a start-up stage (panel up, web up, frames); nil: logged
	// with the time since Run started.
	Mark func(stage string)
	// Product is the firmware line the SYS tab names; Version the app's
	// version the web reports.
	Product, Version string
	// MemoryLimit is the Go runtime's soft memory limit (0: none); a
	// GOMEMLIMIT in the environment wins.
	MemoryLimit int64

	// Defaults is the instrument's power-on state for the board's
	// channels; Check refuses a state without hardware (nil: none).
	// Both are the HAL path's: an Owner keeps its own.
	Defaults func(channels int) instrument.State
	Check    func(instrument.State) error
	// Publish hears every frame the core's owner loop publishes.
	Publish func(*sample.Frame)
	// Owner is a scope's own acquisition owner, run in place of the core's
	// HAL loop until the scope is HAL terms throughout.
	Owner Owner

	// UI builds the UI model; nil: over the instrument state through the
	// board (panel.HALScope).
	UI func(env *Env) (*panel.Controller, error)
	// Input is the front panel the UI model takes its events from; nil:
	// the board's. It runs on its own goroutine when it has a Run(stop).
	Input panel.Input
	// Display is how the scope's display is drawn.
	Display Display

	// SCPI builds the SCPI engine with the scope's dialect; nil: none.
	SCPI func(env *Env) (SCPI, error)
	// SCPIHost names the LAN transports it is served on; a telnet
	// interface without a banner greets with the unit's vendor and model.
	SCPIHost scpihost.Config

	// Listen is the web server's address; "" serves none.
	Listen string
	// Web builds the web scope; nil: the core's web scope over the board,
	// the instrument state and the UI model, with its capabilities.
	Web        func(env *Env) (webserver.Scope, error)
	WebOptions []webserver.Option

	// Settings is how the setup is kept.
	Settings Settings

	// Started hears the whole composition once every part runs.
	Started func(env *Env)
	// OnStop runs after the owner has stopped (a worker to close).
	OnStop func()
}

// Settings is how the setup is kept across a restart: in the board's
// settings store under settings.FileKey, the numbered setups in its setups
// store. Every field may be nil.
type Settings struct {
	// Analog is the front end the setup collects and restores; nil: the
	// HAL scope's (the Model's channels).
	Analog settings.Analog
	// Extension is the board's part of the setup; nil: the level in volts
	// in the Model (settings.ModelLevel).
	Extension settings.Extension
	// SlotKey names slot n in the setups store; nil: its number.
	SlotKey func(n int) string
}

// Owner is a scope's own acquisition owner: its controls, the frames it
// publishes (the fan-out's one source) and its loop. Beats, when it has
// it, is the health token's heartbeat.
type Owner interface {
	instrument.Controls
	fanout.Source[sample.Frame]
	Run()                            // the owner loop, until Stop
	Stop(timeout time.Duration) bool // false: it did not stop in time
}

// SCPI is the scope's SCPI engine as the composition wires it.
type SCPI struct {
	// Exec runs one program message.
	Exec func(msg []byte) []byte
	// Report hears the instrument's refusals (a HAL scope's error queue).
	Report func(error)
	// Setups wires *SAV/*RCL to the setup memory once it exists.
	Setups func(*panel.SharedSetups)
}

// Env is the composition as far as it is built, for the builders.
type Env struct {
	Board    hal.Board
	Identity hal.Identity
	Logf     func(format string, a ...any)
	Product  string
	Version  string
	// Model is the instrument state on the HAL path (nil with an Owner);
	// Controls is what every controller stages through.
	Model    *instrument.Model
	Controls instrument.Controls
	Frames   *fanout.Fanout[sample.Frame]
	// HALScope is the UI model's view of the board (nil with an Owner).
	HALScope *panel.HALScope
	UI       *panel.Controller
	SCPI     SCPI
	// Setups is the setup memory the panel, SCPI and the web share (set
	// after the settings are restored).
	Setups *panel.SharedSetups
	// Web is the web server (set once it is built).
	Web *webserver.Server

	display *display
}

// Render draws the display's picture headless into dst.
// TRLC-LINKS: REQ-DSO-168
func (e *Env) Render(dst render.Canvas) { e.display.Render(dst) }

// Canvas is a fresh canvas the size of the display.
// TRLC-LINKS: REQ-DSO-168
func (e *Env) Canvas() render.Canvas { return e.display.canvas() }

// ScreenPNG is the display's picture as a PNG (the web's screen.png, a
// hardcopy).
// TRLC-LINKS: REQ-DSO-168
func (e *Env) ScreenPNG() []byte {
	c := e.Canvas()
	e.Render(c)
	return render.EncodePNG(c)
}

// webHeaderTimeout bounds a request's header only: the frame route parks a
// request up to 2 s (its long poll), so a global write timeout would kill
// every parked request; slow peers are bounded per response inside the
// handler (SetWriteDeadline).
const webHeaderTimeout = 10 * time.Second

// newWebServer is the HTTP server of h at listen.
func newWebServer(listen string, h http.Handler) *http.Server {
	return &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: webHeaderTimeout}
}

// Run is the scope's application on board b until ctx ends; it returns an
// error when the board does not come up.
// TRLC-LINKS: REQ-DSO-295
func Run(ctx context.Context, b hal.Board, o Options) error { //nolint:gocyclo // the composition, in the order its parts depend on each other
	start := time.Now()
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	mark := o.Mark
	if mark == nil {
		mark = func(stage string) { logf("start: %s at %v", stage, time.Since(start).Round(time.Millisecond)) }
	}
	if o.MemoryLimit > 0 && os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(o.MemoryLimit)
	}

	id, err := b.Platform().BringUp(ctx)
	if err != nil {
		return err
	}
	logf("unit: %s %s serial %s", id.Vendor, id.Model, id.Serial)
	env := &Env{Board: b, Identity: id, Logf: logf, Product: o.Product, Version: o.Version}
	stop := ctx.Done()

	// The instrument and its owner loop.
	h := health{}
	env.Frames = fanout.NewFrames()
	if o.Owner != nil {
		env.Controls = o.Owner
		h.snap = o.Owner.Snapshot
		if bt, ok := o.Owner.(interface{ Beats() uint64 }); ok {
			h.beats = bt.Beats
		}
	} else {
		if o.Defaults == nil {
			return errors.New("scopeapp: a HAL scope needs its default state")
		}
		m := instrument.New(o.Defaults(b.Profile().Channels), o.Check)
		env.Model, env.Controls = m, m
		h.snap = m.Snapshot
		h.beats = func() uint64 { return m.Snapshot().Published }
	}
	if tok := b.Platform().Handover().HealthTok; tok != "" && h.beats != nil {
		go writeHealth(tok, h, stop, logf, mark)
	}
	if o.Owner != nil {
		go o.Owner.Run()
		go env.Frames.Run(o.Owner, stop)
	} else {
		loop, cycle := engine.NewHALLoop(engine.HALConfig{Board: b, Instrument: env.Model, Logf: logf, Publish: o.Publish})
		env.Model.SetFrames(env.Frames)
		go env.Frames.Run(cycle, stop)
		go loop.Run(ctx)
	}

	// The UI model over the front panel.
	if err := buildUI(env, o); err != nil {
		return err
	}
	in := o.Input
	if in == nil {
		in = b.Input()
	}
	if r, ok := in.(interface{ Run(stop <-chan struct{}) }); ok {
		go r.Run(stop)
	}
	go env.UI.Run(stop, in)
	mark("panel up")

	// The display: built now, so SCPI's screen dump and the web's
	// screen.png draw it; it runs once SCPI is wired, which its HUD reads.
	env.display = newDisplay(env, o.Display)

	// SCPI with the scope's dialect.
	if o.SCPI != nil {
		s, err := o.SCPI(env)
		if err != nil {
			return err
		}
		env.SCPI = s
		if s.Report != nil && env.Model != nil {
			env.Model.OnRefused(s.Report)
		}
		if s.Exec != nil {
			cfg := o.SCPIHost
			if cfg.Telnet != "" && cfg.Banner == "" {
				cfg.Banner = "Welcome to the SCPI instrument '" + id.Vendor + " " + id.Model + "'"
			}
			if _, err := scpihost.Serve(ctx, s.Exec, cfg, logf); err != nil {
				logf("scpi: %v", err)
			}
		}
	}
	scr, displayDone := startDisplay(env, o.Display, stop)

	// The setup: restored now that every owner it reaches is up, then
	// saved on change and kept in the setup memory.
	saver := restoreSettings(env, o.Settings)
	if saver != nil {
		go saver.Run(stop)
	}

	// The web server.
	var srv *http.Server
	if o.Listen != "" {
		sc, err := buildWeb(env, o)
		if err != nil {
			return err
		}
		ws := webserver.New(sc, o.WebOptions...)
		env.Web = ws
		srv = newWebServer(o.Listen, ws.Handler())
		go func() {
			logf("web ui listening on %s", o.Listen)
			mark("web up")
			go webserver.Precompress()
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logf("web server: %v", err)
			}
		}()
	}

	if o.Started != nil {
		o.Started(env)
	}
	<-stop
	logf("stop: %v", context.Cause(ctx))
	if saver != nil {
		saver.Flush() // a change still inside the debounce window survives the restart
	}
	// Never leave the panel dark behind the screen saver.
	if s := <-scr; s != nil {
		<-displayDone
		if err := s.SetLight(true); err != nil {
			logf("display: backlight: %v", err)
		}
	}
	if o.Owner != nil && !o.Owner.Stop(2*time.Second) {
		logf("WARNING: engine did not stop in time")
	}
	if o.OnStop != nil {
		o.OnStop()
	}
	if srv != nil {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx) // the process is ending; a slow client is cut off
	}
	return nil
}

// buildUI builds the UI model and wires it to the frames and the screen.
// TRLC-LINKS: REQ-DSO-295
func buildUI(env *Env, o Options) error {
	if o.UI != nil {
		pc, err := o.UI(env)
		if err != nil {
			return err
		}
		env.UI = pc
	} else {
		if env.Model == nil {
			return errors.New("scopeapp: a scope with its own owner builds its UI model")
		}
		s := panel.NewHALScope(env.Model, env.Board, env.Product)
		env.Model.SetTimebases(s.Tdivs())
		env.HALScope = s
		env.UI = panel.New(s.Engine(), s.Analog(), s.Levels(), s.Hardware(), s.Tdivs(), env.Model.State().TimeDivS, env.Logf)
	}
	pc := env.UI
	pc.SetFrameSource(env.Frames.WithFrame)
	spec := env.Board.Profile().Display
	if spec.W > 0 && spec.H > 0 {
		m := metricsFor(spec)
		pc.SetScreen(m.W, m.H)
		if spec.Touch {
			pc.SetTouchKeys(func(x, y int) string { return render.TouchKey(m, x, y) })
		}
	}
	return nil
}

// startDisplay brings the screen up and runs the display loop on its own
// goroutine; the screen (nil without one) comes back on the first channel
// once it is known, and the second closes when the loop has ended.
// TRLC-LINKS: REQ-DSO-168
func startDisplay(env *Env, cfg Display, stop <-chan struct{}) (<-chan Screen, <-chan struct{}) {
	out := make(chan Screen, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		open := cfg.Open
		if open == nil {
			open = func() (Screen, error) { return openBoardScreen(env.Board) }
		}
		scr, err := open()
		if err != nil || scr == nil {
			env.Logf("display: disabled: %v", err)
			out <- nil
			return
		}
		out <- scr
		env.display.run(stop, scr)
	}()
	return out, done
}

// restoreSettings restores the setup in the board's settings store
// through the same setters the panel and the web use (a missing or corrupt
// file is a start with the defaults), wires the setup memory and returns
// the saver of later changes; nil without a store.
// TRLC-LINKS: REQ-DSO-089, REQ-DSO-293
func restoreSettings(env *Env, o Settings) *settings.Saver {
	st := env.Board.Storage()
	if st == nil {
		return nil
	}
	fe, ext := o.Analog, o.Extension
	if fe == nil && env.HALScope != nil {
		fe = env.HALScope.Settings()
	}
	if ext == nil && env.Model != nil {
		ext = settings.ModelLevel{Model: env.Model}
	}
	collect := func() settings.Settings { return settings.Collect(env.Controls, fe, env.UI, ext) }
	apply := func(s settings.Settings) { settings.Apply(s, env.Controls, fe, env.UI, ext, env.Logf) }
	kv := st.Settings()
	var saver *settings.Saver
	if kv != nil {
		if s, ok := settings.Load(kv, settings.FileKey, env.Logf); ok {
			apply(s)
			env.Logf("settings: restored %s", nameOf(kv))
		} else {
			env.Logf("settings: no saved setup at %s — defaults", nameOf(kv))
		}
		saver = settings.NewSaver(kv, settings.FileKey, collect, env.Logf)
	}
	// The numbered setups (*SAV/*RCL, the SAVE/RECALL key), through the
	// same collect and apply as the restore; SCPI and the web save and
	// recall through the panel, so its FILE rows and lamp follow them.
	if setups, ok := st.Setups(); ok && setups != nil {
		env.UI.SetSetupMemory(settings.Slots{KV: setups, Key: o.SlotKey, Collect: collect, Apply: apply})
	}
	env.Setups = env.UI.SharedSetups()
	if env.SCPI.Setups != nil {
		env.SCPI.Setups(env.Setups)
	}
	return saver
}

// nameOf is where the settings file lives, for the log.
func nameOf(kv hal.KV) string {
	if n, ok := kv.(interface{ Name(string) string }); ok {
		return n.Name(settings.FileKey)
	}
	return settings.FileKey
}

// buildWeb is the web scope: the scope's, or the core's over the board,
// the instrument state and the UI model.
// TRLC-LINKS: REQ-DSO-295
func buildWeb(env *Env, o Options) (webserver.Scope, error) {
	if o.Web != nil {
		return o.Web(env)
	}
	if env.Model == nil {
		return nil, errors.New("scopeapp: a scope with its own owner builds its web scope")
	}
	return scopeweb.New(scopeweb.Config{
		Board: env.Board, Instrument: env.Model, Frames: env.Frames, Version: env.Version, UI: env.UI,
		Setups: WebSetups{env.Setups},
	}).WithCaps(), nil
}

// WebSetups is the setup memory the panel and SCPI share as the web's
// setups (the top bar's Setups): one memory, whoever saves.
type WebSetups struct{ *panel.SharedSetups }

// SetupSlots says what each slot holds.
// TRLC-LINKS: REQ-DSO-219
func (w WebSetups) SetupSlots() []wire.SetupSlot {
	var out []wire.SetupSlot
	for _, s := range w.Slots() {
		out = append(out, wire.SetupSlot{Slot: s.Slot, Used: s.Used, Summary: s.Summary})
	}
	return out
}
