// Package simdemo is the "try it before you load it" demo: a scope's whole
// product (the engine, the UI model, the display's renderer and the web
// server) composed by scopeapp.Run over the simulated board (hal/simboard)
// with a signal generator on its inputs. A scope gives only its data: its
// profile and a demo file (identity, code scale, V/div ladder, the signals
// at start, the start state and the front panel's layout for the page).
//
// The demo is a separate tool, never a mode of a scope's main: the host is
// either a plain web server (cmd/dsodemo) or a browser tab running the
// demo as WebAssembly, whose service worker answers the web page's API
// requests by calling the handler here (cmd/dsodemo-wasm).
package simdemo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/simboard"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/profile"
	"github.com/labeth/open-dso-core/scopeapp"
)

// Demo is a scope's demo file (demo.json): what the profile does not say
// about simulating the scope, and the page's front panel.
type Demo struct {
	// Name is the scope's name on the page; Product the firmware line the
	// SYS tab names.
	Name    string `json:"name"`
	Product string `json:"product"`
	// Identity is the simulated unit's; it must match a profile variant.
	Identity struct {
		Vendor string `json:"vendor"`
		Model  string `json:"model"`
		Serial string `json:"serial"`
	} `json:"identity"`
	CodesPerDiv       float64   `json:"codes_per_div"`
	LevelStepsPerDiv  float64   `json:"level_steps_per_div"`
	OffsetStepsPerDiv float64   `json:"offset_steps_per_div"`
	Ladder            []float64 `json:"ladder"`
	Depth             int       `json:"depth"`
	// Signals are the inputs' signals at start, by input name ("C1").
	Signals map[string]string `json:"signals"`
	// Start is the instrument's power-on state.
	Start struct {
		TimeDivS   float64 `json:"tdiv"`
		VoltsDiv   float64 `json:"volts_div"`
		ChannelsOn []int   `json:"channels_on"` // 1-based
		TrigLevelV float64 `json:"trig_level"`  // the edge trigger's level on C1
	} `json:"start"`
	// Panel is the front panel's layout for the page; the demo does not
	// read it.
	Panel json.RawMessage `json:"panel"`
}

// ParseDemo reads a demo file.
// TRLC-LINKS: REQ-DSO-295
func ParseDemo(data []byte) (Demo, error) {
	var d Demo
	if err := json.Unmarshal(data, &d); err != nil {
		return Demo{}, fmt.Errorf("demo file: %w", err)
	}
	return d, nil
}

// Options are the host's part.
type Options struct {
	Logf func(format string, a ...any)
	// Version is the build's version the web reports.
	Version string
	// Listen serves the web on an address (the native host); Serve hands
	// the handler to a host that serves it itself (the browser's worker).
	Listen string
	Serve  func(h http.Handler)
	// Stored, Persist, OnPresent and OnLamps are the board's (simboard.Config).
	Stored    map[string]map[string][]byte
	Persist   func(store, key string, val []byte)
	OnPresent func(rgba []byte, w, h int)
	OnLamps   func(on []string)
	// Started hears the board and the composition once everything runs.
	Started func(b *simboard.Board, env *scopeapp.Env)
}

// Board builds the simulated board of a scope's profile and demo file.
// TRLC-LINKS: REQ-DSO-295, REQ-DSO-1001
func Board(profileYAML []byte, d Demo, o Options) (*simboard.Board, error) {
	id := hal.Identity{Vendor: d.Identity.Vendor, Model: d.Identity.Model, Serial: d.Identity.Serial}
	if id.Vendor == "" {
		id.Vendor = "open-dso"
	}
	if id.Serial == "" {
		id.Serial = "SIM0"
	}
	p, err := profile.Load(profileYAML, id)
	if err != nil {
		return nil, err
	}
	b, err := simboard.New(simboard.Config{
		Profile: p, Identity: id, CodesPerDiv: d.CodesPerDiv,
		LevelStepsPerDiv: d.LevelStepsPerDiv, OffsetStepsPerDiv: d.OffsetStepsPerDiv,
		Ladder: d.Ladder, Depth: d.Depth,
		Stored: o.Stored, Persist: o.Persist, OnPresent: o.OnPresent, OnLamps: o.OnLamps,
	})
	if err != nil {
		return nil, err
	}
	specs := map[int]string{}
	for name, spec := range d.Signals {
		n, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(name), "C"))
		if err != nil || n < 1 || n > p.Channels {
			return nil, fmt.Errorf("demo file: signal input %q: want C1 … C%d", name, p.Channels)
		}
		specs[n-1] = spec
	}
	if err := b.Signals().SetMany(specs); err != nil {
		return nil, fmt.Errorf("demo file: %w", err)
	}
	return b, nil
}

// Defaults is the demo's power-on state over n channels: running in AUTO,
// a rising edge on C1 at the start's level.
// TRLC-LINKS: REQ-DSO-295
func (d Demo) Defaults(n int) instrument.State {
	st := instrument.State{
		TimeDivS: d.Start.TimeDivS, Running: true, Sweep: hal.Auto,
		Trigger: instrument.Edge{Source: hal.Source{Kind: hal.Analog}, Slope: hal.Rising, LevelV: d.Start.TrigLevelV},
	}
	if st.TimeDivS <= 0 {
		st.TimeDivS = 1e-3
	}
	vdiv := d.Start.VoltsDiv
	if vdiv <= 0 {
		vdiv = 1
	}
	on := map[int]bool{}
	for _, c := range d.Start.ChannelsOn {
		on[c-1] = true
	}
	for c := range n {
		st.Channels = append(st.Channels, hal.Channel{On: len(on) == 0 || on[c], VoltsDiv: vdiv, Probe: 1, Impedance: 1e6})
	}
	return st
}

// Run is the demo of a scope's profile and demo file until ctx ends.
// TRLC-LINKS: REQ-DSO-295
func Run(ctx context.Context, profileYAML []byte, d Demo, o Options) error {
	b, err := Board(profileYAML, d, o)
	if err != nil {
		return err
	}
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	product := d.Product
	if product == "" {
		product = d.Name + " demo"
	}
	return scopeapp.Run(ctx, b, scopeapp.Options{
		Logf: logf, Product: product, Version: o.Version,
		Defaults: d.Defaults, Check: check,
		Listen: o.Listen, Serve: o.Serve,
		Display: scopeapp.Display{Engine: pacer{b: b, period: displayPeriod(b.Profile().Budget.DisplayHz)}},
		Started: func(env *scopeapp.Env) {
			if o.Started != nil {
				o.Started(b, env)
			}
		},
	})
}

// pacer is what the display loop asks of the simulated acquisition: its
// cadence, and a least time between records while the display falls
// behind. In a browser the demo has one thread, so without it the
// acquisition takes every turn and the screen starves.
type pacer struct {
	b      *simboard.Board
	period time.Duration
}

// RenderPeriod implements scopeapp.DisplayEngine.
// TRLC-LINKS: REQ-DSO-168
func (p pacer) RenderPeriod() time.Duration { return p.period }

// SetDisplayPace implements scopeapp.DisplayEngine.
// TRLC-LINKS: REQ-DSO-168
func (p pacer) SetDisplayPace(d time.Duration) { p.b.SetPace(d) }

// QuietRLock implements scopeapp.DisplayEngine: the simulated acquisition
// has no load-sensitive window.
// TRLC-LINKS: REQ-DSO-168
func (pacer) QuietRLock() {}

// QuietRUnlock implements scopeapp.DisplayEngine.
// TRLC-LINKS: REQ-DSO-168
func (pacer) QuietRUnlock() {}

// NoteLCDFrame implements scopeapp.DisplayEngine.
// TRLC-LINKS: REQ-DSO-168
func (pacer) NoteLCDFrame() {}

// maxDisplayHz bounds the demo's display cadence: the page shows at most
// this many pictures a second.
const maxDisplayHz = 30

// displayPeriod is the display's tick at the scope's fixed cadence, no
// faster than maxDisplayHz.
// TRLC-LINKS: REQ-DSO-168
func displayPeriod(hz float64) time.Duration {
	if hz <= 0 || hz > maxDisplayHz {
		hz = maxDisplayHz
	}
	return time.Duration(float64(time.Second) / hz)
}

// check refuses a state no board could take.
// TRLC-LINKS: REQ-DSO-295
func check(s instrument.State) error {
	if s.TimeDivS <= 0 {
		return errors.New("time/div must be positive")
	}
	return nil
}

// startTimeout bounds how long Start waits for the composition.
const startTimeout = 30 * time.Second

// Start runs the demo on its own goroutine and returns once it is up, with
// the board and the composition; the demo ends with ctx.
// TRLC-LINKS: REQ-DSO-295
func Start(ctx context.Context, profileYAML []byte, d Demo, o Options) (*simboard.Board, *scopeapp.Env, error) {
	type up struct {
		b   *simboard.Board
		env *scopeapp.Env
	}
	ready := make(chan up, 1)
	done := make(chan error, 1)
	started := o.Started
	o.Started = func(b *simboard.Board, env *scopeapp.Env) {
		if started != nil {
			started(b, env)
		}
		ready <- up{b, env}
	}
	go func() { done <- Run(ctx, profileYAML, d, o) }()
	select {
	case u := <-ready:
		return u.b, u.env, nil
	case err := <-done:
		if err == nil {
			err = errors.New("simdemo: the demo ended before it started")
		}
		return nil, nil, err
	case <-time.After(startTimeout):
		return nil, nil, errors.New("simdemo: the demo did not start")
	}
}
