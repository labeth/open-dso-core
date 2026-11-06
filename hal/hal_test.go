package hal

import (
	"errors"
	"fmt"
	"testing"
)

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

func TestProtoTriggerByEmbedding(t *testing.T) {
	var p ProtoTrigger = uartConfig{Baud: 115200}
	if p.Protocol() != "uart" {
		t.Error(p.Protocol())
	}
}
