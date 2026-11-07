package profile

import (
	"errors"
	"strings"
	"testing"

	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/sample"
)

const base = `# a two-channel test scope
channels: 2
format: {bits: 8, storage: u8, min: 0, max: 255, mid: 128}
rates:
  - samples_per_s: 500e6   # both channels
    max_channels: 2
max_depth: 20480
bandwidth: 100e6
content: [raw-full-rate, peak-pairs]
display:
  w: 800
  h: 480
  format: rgb565
  divs_h: 10
  divs_v: 8
inputs:
  keys: [run, single, "f1"]
  knobs:
  - tdiv
  - 'ch1vdiv'
triggers:
  nodes:
    edge: {slopes: [rising, falling]}
    pulse:
      qual_ops: [shorter, longer]
  sources: [analog]
supervisor: {watchdog: dev-watchdog, restart: watchdog}
options: []
absent:
  logic-probe: "no digital input on this model"
stage: 2
budget:
  ram_bytes: 0x10000000
  quiet_acquire: true
  depth_caps: {eye: 16384}
timebase:
  tdiv_s: [1e-9, 2e-9, 5e-9]
`

func doc(variants string) string { return base + "variants:\n" + variants }

const oneVariant = `  - name: plus
    match: {model: "SDS1102CML+"}
`

// TRLC-LINKS: REQ-DSO-1002
func TestLoadBaseAndVariant(t *testing.T) {
	id := hal.Identity{Vendor: "V", Model: "SDS1102CML+"}
	p, err := Load([]byte(doc(oneVariant+`    set:
      bandwidth: 70e6
      budget: {cores: 1}
`)), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.Identity.Model != id.Model {
		t.Errorf("identity %+v", p.Identity)
	}
	if p.Channels != 2 || p.MaxDepth != 20480 || p.Bandwidth != 70e6 {
		t.Errorf("channels %d depth %d bandwidth %g", p.Channels, p.MaxDepth, p.Bandwidth)
	}
	want := sample.CodeFormat{Bits: 8, Storage: sample.U8, Max: 255, Mid: 128}
	if p.Format != want {
		t.Errorf("format %+v, want %+v", p.Format, want)
	}
	if !p.Content.Has(hal.RawFullRate) || !p.Content.Has(hal.PeakPairs) || p.Content.Has(hal.Resampled) {
		t.Errorf("content %v", p.Content)
	}
	if p.Display.Format != hal.RGB565 || p.Display.W != 800 || p.Display.DivsV != 8 {
		t.Errorf("display %+v", p.Display)
	}
	if strings.Join(p.Inputs.Keys, ",") != "run,single,f1" || strings.Join(p.Inputs.Knobs, ",") != "tdiv,ch1vdiv" {
		t.Errorf("inputs %+v", p.Inputs)
	}
	if !p.Triggers.Has(hal.NodeEdge) || !p.Triggers.Has(hal.NodePulse) || p.Triggers.Has(hal.NodeZone) {
		t.Errorf("trigger nodes %v", p.Triggers.Nodes)
	}
	if got := p.Triggers.Nodes[hal.NodeEdge].Slopes; len(got) != 2 || got[1] != hal.Falling {
		t.Errorf("edge slopes %v", got)
	}
	if p.Supervisor.Watchdog != hal.DevWatchdog {
		t.Errorf("watchdog %v", p.Supervisor.Watchdog)
	}
	// The variant's budget merges into the base's, key by key.
	if p.Budget.RAMBytes != 0x10000000 || !p.Budget.QuietAcquire || p.Budget.Cores != 1 || p.Budget.DepthCaps["eye"] != 16384 {
		t.Errorf("budget %+v", p.Budget)
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestUnknownIdentityRefused(t *testing.T) {
	_, err := Load([]byte(doc(oneVariant)), hal.Identity{Model: "SDS1102CML"})
	r, ok := hal.AsRefusal(err)
	if !ok || r.Feature != "identity" {
		t.Fatalf("err = %v, want an identity refusal", err)
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestOrderedOverlayAndConflicts(t *testing.T) {
	id := hal.Identity{Model: "M1", HWRevision: "B", Fitted: map[string]string{"sram": "present"}, Licences: []string{"decode"}}
	cases := []struct {
		name, variants, conflict string
		bandwidth                float64
		cores                    int
	}{
		{"disjoint settings combine", `  - name: a
    match: {model: "M*"}
    set: {bandwidth: 50e6}
  - name: b
    match: {hw_revision: B}
    set: {budget: {cores: 2}}
`, "", 50e6, 2},
		{"equal values agree", `  - name: a
    match: {model: M1}
    set: {bandwidth: 50e6}
  - name: b
    match: {fitted: {sram: present}}
    set: {bandwidth: 50e6}
`, "", 50e6, 0},
		{"different values conflict", `  - name: a
    match: {model: M1}
    set: {bandwidth: 50e6}
  - name: b
    match: {licences: [decode]}
    set: {bandwidth: 70e6}
`, "bandwidth", 0, 0},
		{"nested settings combine leaf by leaf", `  - name: a
    match: {model: M1}
    set: {budget: {depth_caps: {eye: 1}}}
  - name: b
    match: {model: M1}
    set:
      budget:
        depth_caps:
          eye: 1
          fft: 2
`, "", 100e6, 0},
		{"a setting against settings inside it conflicts", `  - name: a
    match: {model: M1}
    set: {budget: {depth_caps: {eye: 1}}}
  - name: b
    match: {model: M1}
    set: {budget: {depth_caps: ~}}
`, "budget.depth_caps", 0, 0},
		{"a list is one setting", `  - name: a
    match: {model: M1}
    set: {rates: [{samples_per_s: 1e9, max_channels: 1}]}
  - name: b
    match: {model: M1}
    set: {rates: [{samples_per_s: 1e9, max_channels: 2}]}
`, "rates", 0, 0},
		{"a non-matching variant is not applied", `  - name: a
    match: {model: M1}
  - name: b
    match: {fitted: {sram: absent}}
    set: {bandwidth: 1}
`, "", 100e6, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Load([]byte(doc(c.variants)), id)
			if c.conflict != "" {
				if err == nil || !strings.Contains(err.Error(), "both set "+c.conflict) {
					t.Fatalf("err = %v, want a conflict on %s", err, c.conflict)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Bandwidth != c.bandwidth || p.Budget.Cores != c.cores {
				t.Errorf("bandwidth %g cores %d, want %g %d", p.Bandwidth, p.Budget.Cores, c.bandwidth, c.cores)
			}
		})
	}
}

// A later variant overrides the base: the overlay is ordered base first.
// TRLC-LINKS: REQ-DSO-1002
func TestVariantOverridesBase(t *testing.T) {
	p, err := Load([]byte(doc(oneVariant+"    set: {channels: 1, rates: [{samples_per_s: 1e9, max_channels: 1}]}\n")), hal.Identity{Model: "SDS1102CML+"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Channels != 1 || len(p.Rates) != 1 || p.Rates[0].SamplesPerS != 1e9 {
		t.Errorf("channels %d rates %+v", p.Channels, p.Rates)
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestParseErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"unknown key", base + "colour: red\nvariants:\n" + oneVariant, "colour (line"},
		{"unknown key in a variant", doc(oneVariant + "    set: {colour: 1}\n"), "variants.plus.set.colour"},
		{"no variants", base, "no variants"},
		{"variant without match", doc("  - name: a\n"), "no match"},
		{"unknown match field", doc("  - name: a\n    match: {colour: red}\n"), "unknown identity field"},
		{"duplicate variant", doc(oneVariant + oneVariant), "twice"},
		{"identity is not a setting", "identity: {model: x}\n" + doc(oneVariant), "identity (line"},
		{"bad number", doc(oneVariant + "    set: {channels: two}\n"), "not an integer"},
		{"quoted number", doc(oneVariant + "    set: {channels: \"2\"}\n"), "not an integer"},
		{"bad enum", doc(oneVariant + "    set: {display: {format: rgb555}}\n"), "unknown pixel format"},
		{"anchor", doc(oneVariant + "    set: &a {channels: 2}\n"), "outside the profile's YAML subset"},
		{"block scalar", doc(oneVariant + "    set: |\n"), "outside the profile's YAML subset"},
		{"tab", doc(oneVariant + "\tset: {}\n"), "tab"},
		{"duplicate key", "channels: 1\nchannels: 2\n", "duplicate key"},
		{"bad indentation", "channels: 1\n  max_depth: 2\n", "unexpected indentation"},
		{"two documents", base + "---\n", "one document"},
		{"unclosed flow", "a: [1, 2\n", "unclosed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.src))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestValidate(t *testing.T) {
	cases := []struct{ name, set, want string }{
		{"rate for too many channels", "{rates: [{samples_per_s: 1e9, max_channels: 3}]}", "rates[0]"},
		{"format", "{format: {bits: 12, storage: u8, max: 255}}", "do not fit"},
		{"stage", "{stage: 3}", "stage 3"},
		{"timebase order", "{timebase: {tdiv_s: [2e-9, 1e-9]}}", "ascending"},
		{"absent part", "{absent: {probe: \"none\"}}", "absent.probe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load([]byte(doc(oneVariant+"    set: "+c.set+"\n")), hal.Identity{Model: "SDS1102CML+"})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if _, ok := hal.AsRefusal(err); ok {
				t.Fatal("a broken profile is a load error, not a refusal")
			}
		})
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestVariants(t *testing.T) {
	d, err := Parse([]byte(doc(oneVariant + "  - name: other\n    match: {model: X}\n")))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(d.Variants(), ","); got != "plus,other" {
		t.Errorf("variants %s", got)
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestYAMLSubset(t *testing.T) {
	n, err := parseYAML([]byte(`---
a: 'it''s # not a comment'
b: "tab\there" # a comment
c:
- x: 1
  y: [1, "2", {k: v}]
- - nested
- plain words: with colon:inside
d: ~
e:
f: {}
g: [1, 2,
  3, {k: "a]"},
  4]
`))
	if err != nil {
		t.Fatal(err)
	}
	if v := n.m["a"]; v.value != "it's # not a comment" || !v.quoted {
		t.Errorf("a = %+v", v)
	}
	if v := n.m["b"]; v.value != "tab\there" {
		t.Errorf("b = %q", v.value)
	}
	c := n.m["c"]
	if c.kind != seqNode || len(c.items) != 3 {
		t.Fatalf("c = %+v", c)
	}
	if y := c.items[0].m["y"]; len(y.items) != 3 || y.items[2].m["k"].value != "v" || !y.items[1].quoted {
		t.Errorf("c[0].y = %+v", y)
	}
	if c.items[1].kind != scalarNode || c.items[1].value != "- nested" {
		// "- - x" is a plain scalar in this subset: nested block lists
		// are written on their own lines.
		t.Errorf("c[1] = %+v", c.items[1])
	}
	if v := c.items[2].m["plain words"]; v.value != "with colon:inside" {
		t.Errorf("c[2] = %+v", c.items[2])
	}
	if g := n.m["g"]; len(g.items) != 5 || g.items[3].m["k"].value != "a]" || g.items[4].value != "4" {
		t.Errorf("g = %+v", g)
	}
	if n.m["d"].kind != nullNode || n.m["e"].kind != nullNode || n.m["f"].kind != mapNode {
		t.Errorf("d, e, f = %v %v %v", n.m["d"].kind, n.m["e"].kind, n.m["f"].kind)
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestRefusalUnwraps(t *testing.T) {
	_, err := Load([]byte(doc(oneVariant)), hal.Identity{})
	if _, ok := errors.AsType[*hal.Refusal](err); !ok {
		t.Fatalf("err %v", err)
	}
}

// TRLC-LINKS: REQ-DSO-1002
func TestAbsentReasons(t *testing.T) {
	p, err := Load([]byte(doc(oneVariant)), hal.Identity{Model: "SDS1102CML+"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.AbsentReason("logic-probe"); got != "no digital input on this model" {
		t.Errorf("logic-probe reason %q", got)
	}
	if got := p.AbsentReason("generator"); got != "this scope has no generator" {
		t.Errorf("generator reason %q", got)
	}
}
