package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/hal/halfake"
	"github.com/labeth/open-dso-core/instrument"
)

// recBoard records every trigger it is asked to program.
type recBoard struct {
	*halfake.Board
	mu    *sync.Mutex
	specs *[]hal.TriggerSpec
}

func (b recBoard) Trigger() hal.Trigger { return recTrig{b.Board.Trigger(), b.mu, b.specs} }

type recTrig struct {
	hal.Trigger
	mu    *sync.Mutex
	specs *[]hal.TriggerSpec
}

func (t recTrig) Program(s hal.TriggerSpec) error {
	t.mu.Lock()
	*t.specs = append(*t.specs, s)
	t.mu.Unlock()
	return t.Trigger.Program(s)
}

// A type a host interface set in its own levels and limits is programmed
// as it is, not placed on a learned swing; one the board refuses is
// reported with the board's reason and the state goes back.
// TRLC-LINKS: REQ-DSO-011
func TestHALProgramsExplicitTypes(t *testing.T) {
	var mu sync.Mutex
	var specs []hal.TriggerSpec
	b := recBoard{halfake.New(halfake.Config{}), &mu, &specs}
	inst := instrument.New(loopState(hal.Auto), nil)
	var heard []error
	inst.OnRefused(func(err error) { mu.Lock(); heard = append(heard, err); mu.Unlock() })
	ctx, cancel := context.WithCancel(context.Background())
	l, _ := NewHALLoop(HALConfig{Board: b, Instrument: inst, IdleWait: time.Millisecond, WaitSlice: time.Millisecond})
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	_ = inst.Change(func(s *instrument.State) error {
		s.Trigger.Type = instrument.TrigPulse
		if err := s.SetTrigParam(instrument.TrigPulse, "level", instrument.ParamValue{Num: 0.3}); err != nil {
			return err
		}
		return s.SetTrigParam(instrument.TrigPulse, "limit", instrument.ParamValue{Text: "greater"})
	})
	until := func(fn func() bool) bool {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			mu.Lock()
			ok := fn()
			mu.Unlock()
			if ok {
				return true
			}
		}
		return false
	}
	if !until(func() bool {
		for _, s := range specs {
			if len(s.Stages) == 1 && s.Stages[0].Node == hal.NodePulse && s.Stages[0].Levels.High == 0.3 &&
				s.Stages[0].Qual != nil && s.Stages[0].Qual.Op == hal.Longer && !s.FreeRun {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("no pulse programmed as set: %+v", specs)
	}
	_ = inst.Change(func(s *instrument.State) error { s.Trigger.Type = instrument.TrigWindow; return nil })
	if !until(func() bool { return len(heard) > 0 }) || !strings.Contains(heard[0].Error(), "no such trigger node") {
		t.Fatalf("refusals heard: %v", heard)
	}
	if !until(func() bool { return inst.State().Trigger.Type == instrument.TrigPulse }) {
		t.Fatalf("the type stays %v after the refusal", inst.State().Trigger.Type)
	}
}
