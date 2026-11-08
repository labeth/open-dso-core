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
	"github.com/labeth/open-dso-core/hal/keytable"
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
	// RecordDepth is the deepest record the simulated board samples (a
	// full-rate capture memory deeper than the profile's readout window, or
	// a memory bounded for a browser); 0: the profile's.
	RecordDepth int `json:"record_depth"`
	// RecordScreens is how many screens a live record spans where the
	// timebase decimates (simboard.Config.RecordScreens).
	RecordScreens float64 `json:"record_screens"`
	// Signals are the inputs' signals at start, by input name ("C1").
	Signals map[string]string `json:"signals"`
	// Start is the instrument's power-on state.
	Start struct {
		TimeDivS   float64 `json:"tdiv"`
		VoltsDiv   float64 `json:"volts_div"`
		ChannelsOn []int   `json:"channels_on"` // 1-based
		TrigLevelV float64 `json:"trig_level"`  // the edge trigger's level on C1
	} `json:"start"`
	// Panel is the front panel's layout for the page, by the panel's own
	// control names (the key table's); the demo checks it against the key
	// table (CheckPanel).
	Panel json.RawMessage `json:"panel"`
	// KeyTable is the board's key table (the scope's board states it, and
	// the scope's test keeps it here): the simulated board makes the
	// events the real panel makes; empty: the profile's names, a key
	// pressed and released.
	KeyTable keytable.Table `json:"keytable"`
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
	if !d.KeyTable.Empty() {
		if err := d.KeyTable.CheckProfile(p.Inputs); err != nil {
			return nil, fmt.Errorf("demo file: key table: %w", err)
		}
		if err := d.CheckPanel(); err != nil {
			return nil, fmt.Errorf("demo file: %w", err)
		}
	}
	if p.Budget.DisplayHz > maxDisplayHz {
		p.Budget.DisplayHz = maxDisplayHz
	}
	b, err := simboard.New(simboard.Config{
		Profile: p, Identity: id, CodesPerDiv: d.CodesPerDiv,
		LevelStepsPerDiv: d.LevelStepsPerDiv, OffsetStepsPerDiv: d.OffsetStepsPerDiv,
		Ladder: d.Ladder, Depth: d.Depth, RecordDepth: d.RecordDepth, RecordScreens: d.RecordScreens,
		Stored: o.Stored, Persist: o.Persist, OnPresent: o.OnPresent, OnLamps: o.OnLamps,
		KeyTable: d.KeyTable,
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
	return scopeapp.Run(ctx, b, Compose(d, o, b))
}

// Compose is the demo's composition: a device main's (scopeapp.Run over
// the board, every part the core's) with the host's parts a browser tab
// has. Where it differs from a scope's device main, Differences says why.
// TRLC-LINKS: REQ-DSO-295
func Compose(d Demo, o Options, b *simboard.Board) scopeapp.Options {
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	product := d.Product
	if product == "" {
		product = d.Name + " demo"
	}
	return scopeapp.Options{
		Logf: logf, Name: d.Name, Product: product, Version: o.Version,
		Defaults: d.Defaults, Check: check,
		Listen: o.Listen, Serve: o.Serve,
		Started: func(env *scopeapp.Env) {
			if o.Started != nil {
				o.Started(b, env)
			}
		},
	}
}

// maxDisplayHz bounds the demo's display cadence: the page shows at most
// this many pictures a second (the browser host's presentEvery), and every
// picture is drawn on the one thread the demo shares with the acquisition.
const maxDisplayHz = 30

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
