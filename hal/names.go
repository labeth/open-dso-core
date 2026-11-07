package hal

import (
	"fmt"
	"strings"
)

// Enumerations that a profile names in text implement encoding.TextMarshaler
// and TextUnmarshaler through these tables; the profile loader needs nothing
// else to read them. Index 0 is the zero value's name ("" when the zero
// value has none).

var (
	trigNodeNames = map[TrigNode]string{
		NodeEdge: "edge", NodePulse: "pulse", NodeSlope: "slope", NodeRunt: "runt",
		NodeWindow: "window", NodePattern: "pattern", NodeSetupHold: "setup-hold",
		NodeTimeout: "timeout", NodeVideo: "video", NodeZone: "zone", NodeProtocol: "protocol",
		NodeSymbol: "symbol", NodeSequence: "sequence", NodeFrameStart: "frame-start",
	}
	sourceKindNames = []string{"", "analog", "digital", "external", "ac-line"}
	slopeNames      = []string{"", "rising", "falling", "either"}
	qualOpNames     = []string{"", "shorter", "longer", "inside", "outside"}
	pixelNames      = []string{"", "rgb565", "xrgb8888", "argb8888"}
	watchdogNames   = []string{"none", "dev-watchdog", "heartbeat"}
	optNames        = []string{"logic-probe", "awg", "counter", "segments", "stacker"}
)

// TRLC-LINKS: REQ-DSO-1001
func nameOf(names []string, i int, what string) ([]byte, error) {
	if i < 0 || i >= len(names) || names[i] == "" {
		return nil, fmt.Errorf("hal: %s %d has no name", what, i)
	}
	return []byte(names[i]), nil
}

// TRLC-LINKS: REQ-DSO-1001
func indexOf(names []string, b []byte, what string) (int, error) {
	for i, n := range names {
		if n != "" && n == string(b) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("hal: unknown %s %q", what, b)
}

// String returns the node's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (n TrigNode) String() string {
	if s, ok := trigNodeNames[n]; ok {
		return s
	}
	return fmt.Sprintf("node(%d)", uint16(n))
}

// MarshalText returns the node's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (n TrigNode) MarshalText() ([]byte, error) {
	if s, ok := trigNodeNames[n]; ok {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("hal: trigger node %d has no name", n)
}

// UnmarshalText parses a node name such as "frame-start".
// TRLC-LINKS: REQ-DSO-1001
func (n *TrigNode) UnmarshalText(b []byte) error {
	for k, s := range trigNodeNames {
		if s == string(b) {
			*n = k
			return nil
		}
	}
	return fmt.Errorf("hal: unknown trigger node %q", b)
}

// MarshalText returns the kind's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (k SourceKind) MarshalText() ([]byte, error) {
	return nameOf(sourceKindNames, int(k), "source kind")
}

// UnmarshalText parses a source kind name.
// TRLC-LINKS: REQ-DSO-1001
func (k *SourceKind) UnmarshalText(b []byte) error {
	i, err := indexOf(sourceKindNames, b, "source kind")
	*k = SourceKind(i)
	return err
}

// MarshalText returns the slope's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (s Slope) MarshalText() ([]byte, error) { return nameOf(slopeNames, int(s), "slope") }

// UnmarshalText parses a slope name.
// TRLC-LINKS: REQ-DSO-1001
func (s *Slope) UnmarshalText(b []byte) error {
	i, err := indexOf(slopeNames, b, "slope")
	*s = Slope(i)
	return err
}

// MarshalText returns the operator's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (q QualOp) MarshalText() ([]byte, error) { return nameOf(qualOpNames, int(q), "qualifier") }

// UnmarshalText parses a qualifier operator name.
// TRLC-LINKS: REQ-DSO-1001
func (q *QualOp) UnmarshalText(b []byte) error {
	i, err := indexOf(qualOpNames, b, "qualifier")
	*q = QualOp(i)
	return err
}

// MarshalText returns the format's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (p PixelFormat) MarshalText() ([]byte, error) {
	return nameOf(pixelNames, int(p), "pixel format")
}

// UnmarshalText parses a pixel format name.
// TRLC-LINKS: REQ-DSO-1001
func (p *PixelFormat) UnmarshalText(b []byte) error {
	i, err := indexOf(pixelNames, b, "pixel format")
	*p = PixelFormat(i)
	return err
}

// MarshalText returns the watchdog kind's profile name.
// TRLC-LINKS: REQ-DSO-1001
func (w WatchdogKind) MarshalText() ([]byte, error) {
	return nameOf(watchdogNames, int(w), "watchdog")
}

// UnmarshalText parses a watchdog kind name.
// TRLC-LINKS: REQ-DSO-1001
func (w *WatchdogKind) UnmarshalText(b []byte) error {
	i, err := indexOf(watchdogNames, b, "watchdog")
	*w = WatchdogKind(i)
	return err
}

// MarshalText lists the options' names, comma separated.
// TRLC-LINKS: REQ-DSO-1001
func (o OptSet) MarshalText() ([]byte, error) {
	var names []string
	for i, n := range optNames {
		if o&(1<<i) != 0 {
			names = append(names, n)
		}
	}
	return []byte(strings.Join(names, ",")), nil
}

// UnmarshalText parses comma-separated option names.
// TRLC-LINKS: REQ-DSO-1001
func (o *OptSet) UnmarshalText(b []byte) error {
	var out OptSet
	for f := range strings.SplitSeq(string(b), ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		i, err := indexOf(optNames, []byte(f), "option")
		if err != nil {
			return err
		}
		out |= 1 << i
	}
	*o = out
	return nil
}
