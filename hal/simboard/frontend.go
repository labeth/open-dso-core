package simboard

import (
	"context"
	"fmt"
	"math"

	"github.com/labeth/open-dso-core/hal"
)

// offsetDivs is the offset range, in divisions each way.
const offsetDivs = 10

type frontEnd struct{ b *Board }

// Set snaps c to the ladder at its probe factor and the offset to its
// grid; a V/div outside the ladder is refused.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Set(ch int, c hal.Channel) (hal.Channel, error) {
	b := fe.b
	if ch < 0 || ch >= b.profile.Channels {
		return hal.Channel{}, hal.Refuse(fmt.Sprintf("channel %d", ch+1), "this scope has no such channel")
	}
	if c.Probe <= 0 {
		c.Probe = 1
	}
	lad := b.cfg.Ladder
	tip := c.VoltsDiv / c.Probe
	if !(tip >= lad[0]*(1-1e-9) && tip <= lad[len(lad)-1]*(1+1e-9)) {
		return hal.Channel{}, hal.Refuse(fmt.Sprintf("%g V/div at %gx", c.VoltsDiv, c.Probe),
			fmt.Sprintf("outside the %g V … %g V ladder", lad[0]*c.Probe, lad[len(lad)-1]*c.Probe))
	}
	best := lad[0]
	for _, v := range lad {
		if math.Abs(math.Log(v/tip)) < math.Abs(math.Log(best/tip)) {
			best = v
		}
	}
	c.VoltsDiv = best * c.Probe
	if c.Impedance == 0 {
		c.Impedance = 1e6
	}
	if c.Impedance != 1e6 {
		return hal.Channel{}, hal.Refuse("input impedance", "the simulated inputs are 1 MΩ only")
	}
	if c.BWLimit < 0 {
		c.BWLimit = 0
	}
	c.OffsetV = b.offsetGrid(c).Snap(c.OffsetV)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.staged.channels[ch] = c
	return c, nil
}

// offsetGrid is the offset's step and range at setting c.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) offsetGrid(c hal.Channel) hal.Grid {
	steps := b.cfg.OffsetStepsPerDiv
	if steps <= 0 {
		steps = b.codeSteps()
	}
	lim := offsetDivs * c.VoltsDiv
	return hal.Grid{Step: c.VoltsDiv / steps, Min: -lim, Max: lim}
}

// Get implements hal.FrontEnd.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Get(ch int) hal.Channel {
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	if ch < 0 || ch >= len(fe.b.applied.channels) {
		return hal.Channel{}
	}
	return fe.b.applied.channels[ch]
}

// Ladder is the V/div ladder at the channel's probe factor.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Ladder(ch int) []hal.Step {
	p := fe.staged(ch).Probe
	if p <= 0 {
		p = 1
	}
	out := make([]hal.Step, len(fe.b.cfg.Ladder))
	for i, v := range fe.b.cfg.Ladder {
		out[i] = hal.Step{VoltsDiv: v * p, Zoom: 1}
	}
	return out
}

// staged is channel ch's staged setting.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) staged(ch int) hal.Channel {
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	if ch < 0 || ch >= len(fe.b.staged.channels) {
		return hal.Channel{}
	}
	return fe.b.staged.channels[ch]
}

// OffsetGrid implements hal.FrontEnd.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) OffsetGrid(ch int) hal.Grid { return fe.b.offsetGrid(fe.staged(ch)) }

// Couplings are DC, AC and GND, all applied to the record.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Couplings(int) []hal.CouplingOption {
	return []hal.CouplingOption{{Coupling: hal.DC}, {Coupling: hal.AC}, {Coupling: hal.GND}}
}

// Calibration implements hal.FrontEnd: the simulated front end is exact.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) Calibration() hal.CalSource {
	fe.b.mu.Lock()
	defer fe.b.mu.Unlock()
	return fe.b.calSource()
}

// calSource names the calibration in use.
// TRLC-LINKS: REQ-DSO-1001
func (b *Board) calSource() hal.CalSource {
	if b.selfCals > 0 {
		return hal.CalSource{Source: "simulated-selfcal", Author: "open", Valid: true}
	}
	return hal.CalSource{Source: "simulated", Valid: true}
}

// SelfCal reports its progress and changes nothing: the simulated front end
// needs no calibration. It refuses while armed, as a board must.
// TRLC-LINKS: REQ-DSO-1001
func (fe frontEnd) SelfCal(ctx context.Context, progress func(float64)) (hal.CalReport, error) {
	b := fe.b
	b.mu.Lock()
	if b.state == hal.Armed || b.state == hal.Reading {
		b.mu.Unlock()
		return hal.CalReport{}, hal.Refuse("self-calibration", "the acquisition runs: stop it first")
	}
	b.mu.Unlock()
	for i := range 5 {
		if err := ctx.Err(); err != nil {
			return hal.CalReport{}, err
		}
		if progress != nil {
			progress(float64(i) / 4)
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.selfCals++
	b.state, b.stopped = hal.Idle, true
	return hal.CalReport{Source: b.calSource()}, nil
}
