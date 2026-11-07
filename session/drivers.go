package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// SimBench drives a simulated board's bench routes (hal/simboard's
// /api/sim/signal, /api/sim/bus and /api/sim/input), on the scope's own
// web server: the native and the WebAssembly demo.
type SimBench struct {
	C *Client
}

// Can reports that the generator produces every signal the format describes.
// TRLC-LINKS: REQ-DSO-295
func (SimBench) Can(map[string]Signal) (bool, string) { return true, "" }

// Apply sets the waveforms and single-line protocols in one request and
// each multi-line bus in one.
// TRLC-LINKS: REQ-DSO-295
func (b SimBench) Apply(ctx context.Context, sigs map[string]Signal) error {
	signals, buses, err := SimRequests(sigs)
	if err != nil {
		return err
	}
	var errs []error
	if len(signals) > 0 {
		errs = append(errs, b.C.Post(ctx, "/api/sim/signal", map[string]any{"signals": signals}))
	}
	for _, bus := range buses {
		errs = append(errs, b.C.Post(ctx, "/api/sim/bus", bus))
	}
	return joinPlanned(errs)
}

// joinPlanned joins errs, reporting a dry run's printed requests as one
// ErrPlanned.
// TRLC-LINKS: REQ-DSO-295
func joinPlanned(errs []error) error {
	var failed []error
	planned := false
	for _, e := range errs {
		switch {
		case errors.Is(e, ErrPlanned):
			planned = true
		case e != nil:
			failed = append(failed, e)
		}
	}
	if len(failed) == 0 && planned {
		return ErrPlanned
	}
	return errors.Join(failed...)
}

// Key presses a key on the simulated board's panel queue.
// TRLC-LINKS: REQ-DSO-295
func (b SimBench) Key(ctx context.Context, name string) error {
	return b.C.Post(ctx, "/api/sim/input", map[string]any{"key": name})
}

// Knob turns a knob on the simulated board's panel queue.
// TRLC-LINKS: REQ-DSO-295
func (b SimBench) Knob(ctx context.Context, name string, steps int) error {
	return b.C.Post(ctx, "/api/sim/input", map[string]any{"knob": name, "steps": steps})
}

// SimLab drives a scope simulator's bench page: one input's signal at a
// time ({"ch":"C1","spec":…} on /api/signal) and a bus's lines together
// (/api/bus), in the generator syntax the demo's uses.
type SimLab struct {
	C *Client // the bench page's server, not the scope's
}

// Can reports that the simulator's generator takes the demo generator's syntax.
// TRLC-LINKS: REQ-DSO-295
func (SimLab) Can(map[string]Signal) (bool, string) { return true, "" }

// Apply sets each waveform input, then each bus.
// TRLC-LINKS: REQ-DSO-295
func (l SimLab) Apply(ctx context.Context, sigs map[string]Signal) error {
	signals, buses, err := SimRequests(sigs)
	if err != nil {
		return err
	}
	var errs []error
	for _, in := range slices.Sorted(maps.Keys(signals)) {
		errs = append(errs, l.C.Post(ctx, "/api/signal", map[string]any{"ch": in, "spec": signals[in]}))
	}
	for _, bus := range buses {
		errs = append(errs, l.C.Post(ctx, "/api/bus", bus))
	}
	return joinPlanned(errs)
}

// Bench is a real scope's bench as data: how it is wired and the sources
// that can drive its inputs, each with the signals it puts on them and
// how to select it.
type Bench struct {
	Name   string   `json:"name"`
	Wiring []string `json:"wiring"` // what must be connected, for the operator
	// Notes are what the operator must know about the bench (the probes'
	// attenuation, a source's levels).
	Notes   []string      `json:"notes,omitempty"`
	Sources []BenchSource `json:"sources"`
}

// BenchSource is one way to drive the inputs.
type BenchSource struct {
	ID string `json:"id"`
	// Provides are the signals the source puts on each input it drives.
	Provides map[string]Signal `json:"provides"`
	// Select are shell commands that select the source (load a bitstream,
	// send a generator command); empty: the operator connects it by hand
	// (Instruct).
	Select   []string `json:"select,omitempty"`
	Instruct string   `json:"instruct,omitempty"`
}

// ParseBench reads a bench file.
// TRLC-LINKS: REQ-DSO-295
func ParseBench(data []byte) (*Bench, error) {
	var b Bench
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	for _, s := range b.Sources {
		for in, sig := range s.Provides {
			if err := sig.Validate(); err != nil {
				return nil, fmt.Errorf("source %s, %s: %w", s.ID, in, err)
			}
		}
	}
	return &b, nil
}

// Source is the bench source that produces every signal of sigs.
// TRLC-LINKS: REQ-DSO-295
func (b *Bench) Source(sigs map[string]Signal) (*BenchSource, bool) {
	for i := range b.Sources {
		src := &b.Sources[i]
		ok := true
		for in, want := range sigs {
			have, drives := src.Provides[in]
			if !drives || !Same(want, have) {
				ok = false
				break
			}
		}
		if ok {
			return src, true
		}
	}
	return nil, false
}

// The ways a bench run selects its sources.
const (
	SourcesRun    = "run"    // run the source's commands
	SourcesAsk    = "ask"    // print what to wire or select and wait for Enter
	SourcesAssume = "assume" // take the source as already selected
)

// BenchDriver selects a bench's sources for a session's signals.
type BenchDriver struct {
	Bench *Bench
	Mode  string    // SourcesRun, SourcesAsk or SourcesAssume
	Plan  io.Writer // non-nil: a dry run, print instead of acting
	Out   io.Writer // the operator's prompts
	In    io.Reader // the operator's Enter
	cur   string    // the source selected last
}

// Can reports whether a source of the bench produces sigs.
// TRLC-LINKS: REQ-DSO-295
func (d *BenchDriver) Can(sigs map[string]Signal) (bool, string) {
	if len(sigs) == 0 {
		return true, ""
	}
	if _, ok := d.Bench.Source(sigs); !ok {
		return false, "no bench source provides " + DescribeSignals(sigs)
	}
	return true, ""
}

// Apply selects the source of sigs unless it is the one selected last.
// TRLC-LINKS: REQ-DSO-295
func (d *BenchDriver) Apply(ctx context.Context, sigs map[string]Signal) error {
	src, ok := d.Bench.Source(sigs)
	if !ok {
		return errors.New("no bench source provides " + DescribeSignals(sigs))
	}
	if src.ID == d.cur {
		return nil
	}
	if d.Plan != nil {
		_, _ = fmt.Fprintf(d.Plan, "    bench source %s (%s): %s\n", src.ID, DescribeSignals(src.Provides), d.Mode)
		for _, c := range src.Select {
			_, _ = fmt.Fprintf(d.Plan, "      $ %s\n", c)
		}
		if src.Instruct != "" {
			_, _ = fmt.Fprintf(d.Plan, "      operator: %s\n", src.Instruct)
		}
		d.cur = src.ID
		return ErrPlanned
	}
	switch d.Mode {
	case SourcesRun:
		for _, c := range src.Select {
			cmd := exec.CommandContext(ctx, "sh", "-c", c) //nolint:gosec // G204: the bench file's own commands, which the operator chose to run
			cmd.Stdout, cmd.Stderr = d.Out, d.Out
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("source %s: %s: %w", src.ID, c, err)
			}
		}
		if len(src.Select) == 0 && src.Instruct != "" {
			return fmt.Errorf("source %s is selected by hand (%s): run with -sources ask", src.ID, src.Instruct)
		}
	case SourcesAsk:
		_, _ = fmt.Fprintf(d.Out, "Select bench source %s: %s\n", src.ID, DescribeSignals(src.Provides))
		for _, c := range src.Select {
			_, _ = fmt.Fprintf(d.Out, "  $ %s\n", c)
		}
		if src.Instruct != "" {
			_, _ = fmt.Fprintln(d.Out, "  "+src.Instruct)
		}
		_, _ = fmt.Fprint(d.Out, "Press Enter when it is selected: ")
		in := d.In
		if in == nil {
			in = os.Stdin
		}
		if _, err := fmt.Fscanln(in); err != nil && !strings.Contains(err.Error(), "unexpected newline") {
			return fmt.Errorf("source %s: %w", src.ID, err)
		}
	case SourcesAssume:
	default:
		return fmt.Errorf("sources mode %q: want run, ask or assume", d.Mode)
	}
	d.cur = src.ID
	return nil
}
