package hal

import (
	"errors"
	"fmt"
	"testing"
)

// TRLC-LINKS: REQ-DSO-1001
func TestRefusalFoundThroughWrapping(t *testing.T) {
	err := fmt.Errorf("commit: %w", Refuse("trigger zone", "this image has no zone node"))
	r, ok := AsRefusal(err)
	if !ok || r.Feature != "trigger zone" {
		t.Fatalf("AsRefusal(%v) = %v, %v", err, r, ok)
	}
	if r.Error() != "trigger zone: this image has no zone node" {
		t.Errorf("text %q", r.Error())
	}
	if _, ok := AsRefusal(ErrTimeout); ok {
		t.Error("a timeout is not a refusal")
	}
	if errors.Is(ErrNotTaken, ErrTimeout) {
		t.Error("sentinels alias")
	}
}

// TRLC-LINKS: REQ-DSO-1001
func TestEnumText(t *testing.T) {
	for n := range trigNodeNames {
		b, err := n.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back TrigNode
		if err := back.UnmarshalText(b); err != nil || back != n {
			t.Errorf("node %v: %v %v", n, back, err)
		}
	}
	var o OptSet
	if err := o.UnmarshalText([]byte("awg, stacker")); err != nil || !o.Has(OptAWG|OptStacker) || o.Has(OptCounter) {
		t.Fatalf("options %b %v", o, err)
	}
	if b, _ := o.MarshalText(); string(b) != "awg,stacker" {
		t.Errorf("options text %q", b)
	}
	var p PixelFormat
	if err := p.UnmarshalText([]byte("argb8888")); err != nil || p != ARGB8888 {
		t.Error(p, err)
	}
	var w WatchdogKind
	if err := w.UnmarshalText([]byte("none")); err != nil || w != NoWatchdog {
		t.Error(w, err)
	}
	var s Slope
	if err := s.UnmarshalText([]byte("sideways")); err == nil {
		t.Error("unknown slope accepted")
	}
}

// TRLC-LINKS: REQ-DSO-1001
func TestRoleView(t *testing.T) {
	for _, r := range []Role{Main, Zoom, Edge, EdgeOpp} {
		if _, ok := r.ViewIndex(); ok {
			t.Errorf("%d is a view role", r)
		}
	}
	if n, ok := RoleView(3).ViewIndex(); !ok || n != 3 {
		t.Errorf("RoleView(3) = %d, %v", n, ok)
	}
}

// TRLC-LINKS: REQ-DSO-1001
func TestGridSnap(t *testing.T) {
	g := Grid{Step: 0.04, Min: -4, Max: 4}
	cases := map[float64]float64{0.05: 0.04, 0.07: 0.08, -10: -4, 10: 4, -0.05: -0.04}
	for in, want := range cases {
		if got := g.Snap(in); got < want-1e-9 || got > want+1e-9 {
			t.Errorf("Snap(%g) = %g, want %g", in, got, want)
		}
	}
}

type uartConfig struct {
	ProtoTriggerType
	Baud float64
}

func (uartConfig) Protocol() string { return "uart" }

// TRLC-LINKS: REQ-DSO-1001
func TestProtoTriggerByEmbedding(t *testing.T) {
	var p ProtoTrigger = uartConfig{Baud: 115200}
	if p.Protocol() != "uart" {
		t.Error(p.Protocol())
	}
}

// TRLC-LINKS: REQ-DSO-1001
func TestStreamBlockRuns(t *testing.T) {
	b := StreamBlock{Payload: PayloadCodes, Formats: make([]CodeFormat, 3), Words: []uint16{1, 2, 3, 4, 5, 6}}
	for i, want := range [][]uint16{{1, 2}, {3, 4}, {5, 6}} {
		if _, w := b.Run(i); len(w) != 2 || w[0] != want[0] || w[1] != want[1] {
			t.Errorf("run %d = %v, want %v", i, w, want)
		}
	}
	if by, w := b.Run(3); by != nil || w != nil {
		t.Error("a run past the channels")
	}
	b8 := StreamBlock{Formats: make([]CodeFormat, 2), Bytes: []byte{7, 8, 9, 10}}
	if by, _ := b8.Run(1); len(by) != 2 || by[0] != 9 {
		t.Errorf("byte run %v", by)
	}
}
