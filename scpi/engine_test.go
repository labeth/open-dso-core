package scpi

import (
	"fmt"
	"strings"
	"testing"
)

// ieee is a small IEEE dialect: the commons, the system subset and two
// channel settings of the product class (missing value: -200).
func ieee(status Summaries) *Engine {
	scale := map[int]float64{1: 1, 2: 1}
	d := Dialect{Name: "test", Grammar: IEEE, Status: status}
	d.Commands = append(d.Commands, Commons{IDN: func() string { return "Maker,Model,SN1,1.0" }}.Commands()...)
	d.Commands = append(d.Commands, System()...)
	d.Commands = append(d.Commands, CommHeader()...)
	d.Commands = append(d.Commands,
		Command{":CHANnel#:SCALe", func(c *Call) error {
			if c.Count() == 0 {
				return ErrFailed
			}
			v, _, err := c.Float(true)
			if err == nil {
				scale[c.N(0)] = v
			}
			return err
		}},
		Command{":CHANnel#:SCALe?", func(c *Call) error { c.NR3(scale[c.N(0)]); return nil }},
		Command{":CHANnel#:OFFSet?", func(c *Call) error { c.NR3(0); return nil }},
		Command{":CHANnel#:COUPling", func(c *Call) error {
			_, _, err := c.Choice(true, "DC", "AC", "GND")
			return err
		}},
		Command{":FORMat:DATA", func(c *Call) error {
			i, _, err := c.Choice(true, "SINGle", "DOUBle", "CUSTom")
			if err != nil {
				return err
			}
			c.SetDigits([]int{7, 14, 3}[i])
			if n, ok, err := c.Int(false); ok && err == nil {
				c.SetDigits(int(n))
			}
			return nil
		}},
		Command{"C#:VDIV?", func(c *Call) error { c.Legacy("C1:VDIV", "C1:VOLT_DIV", FormatNR3(1, c.Digits()), "V"); return nil }},
	)
	d.Header = HeaderRules{Long: true, OffDropsUnit: true}
	return New(d, nil)
}

func run(t *testing.T, e *Engine, cases [][2]string) {
	t.Helper()
	for _, tc := range cases {
		if got := string(e.Exec([]byte(tc[0]))); got != tc[1] {
			t.Errorf("%q: got %q want %q", tc[0], got, tc[1])
		}
	}
}

// The libscpi message algorithm: paths, suffixes, the ';' and LF rules and
// the error queue's information.
// TRLC-LINKS: REQ-DSO-024
func TestIEEEMessages(t *testing.T) {
	run(t, ieee(EdgeSummaries), [][2]string{
		{"*IDN?", "Maker,Model,SN1,1.0\n"},
		{":CHAN1:SCAL?", "1.00E+00\n"},
		{":CHAN1:SCAL?;OFFS?", "1.00E+00;0.00E+00\n"},
		{":CHANnel2:SCALe 500mV", ""},
		{"chan2:scale?", "5.00E-01\n"},
		{":CHAN:SCAL?", "1.00E+00\n"},
		{"*IDN?;*OPC?", "Maker,Model,SN1,1.0;1\n"},
		{"*IDN?;*CLS", "Maker,Model,SN1,1.0"},
		{"*IDN?;*CLS;*OPC?", "Maker,Model,SN1,1.01\n"},
		{"*OPC?;*ESE 0;*OPC?;*ESE?", "11;0\n"},
		{"*OPC?;FOO", "1\n"},
		{":CHAN1:SCAL?;:CHAN1:SCAL 1E-6;:CHAN1:SCAL?", "1.00E+001.00E-06\n"},
		{"*CLS", ""},
		{"FOO:BAR?", ""},
		{"A1;A2", ""},
		{"SYST:ERR?;:SYST:ERR?;:SYST:ERR?", `-113,"Undefined header;FOO:BAR?";-113,"Undefined header;A1;";-113,"Undefined header;A2"` + "\n"},
		{":CHAN1:SCAL", ""},
		{":CHAN1:SCAL 1,2", ""},
		{":CHAN1:COUP XYZ", ""},
		{":CHAN1:SCAL abc", ""},
		{"SYST:COUN?", "4\n"},
		{"SYST:ERR?;ERR?;ERR?;ERR?;ERR?", `-200,"Execution error";-108,"Parameter not allowed";-224,"Illegal parameter value";-104,"Data type error";0,"No error"` + "\n"},
		{"SYSTem:VERSion?", "1999.0\n"},
		{":FORM:DATA SING;:CHAN1:SCAL?", "1.000000E+00\n"},
		{":FORM:DATA CUST,6;:CHAN1:SCAL?", "1.00000E+00\n"},
		{":FORM:DATA CUST,3", ""},
		{"C1:VDIV?", "C1:VDIV 1.00E+00V\n"},
		{"CHDR LONG;C1:VDIV?;:CHDR?", "C1:VOLT_DIV 1.00E+00V;COMM_HEADER LONG\n"},
		{"CHDR OFF;C1:VDIV?;:CHDR?", "1.00E+00;OFF\n"},
		{"CHDR SHORT;CHDR?", "CHDR SHORT\n"},
	})
}

// The error queue keeps 16 errors and the overflow marker.
// TRLC-LINKS: REQ-DSO-024
func TestIEEEQueueOverflow(t *testing.T) {
	e := ieee(EdgeSummaries)
	for i := range 20 {
		e.Exec(fmt.Appendf(nil, "BAD%d", i))
	}
	if got := string(e.Exec([]byte("SYST:COUN?"))); got != "17\n" {
		t.Fatalf("count %q", got)
	}
	for i := range 16 {
		if got, want := string(e.Exec([]byte("SYST:ERR?"))), fmt.Sprintf("-113,\"Undefined header;BAD%d\"\n", i); got != want {
			t.Fatalf("%d: %q", i, got)
		}
	}
	run(t, e, [][2]string{{"SYST:ERR?", "-350,\"Queue overflow\"\n"}, {"SYST:ERR?", "0,\"No error\"\n"}})
}

// The edge rule: ESB follows only a change of ESR, MSS every change of STB
// or SRE; *ESR? and *CLS clear.
// TRLC-LINKS: REQ-DSO-024
func TestStatusEdgeRule(t *testing.T) {
	run(t, ieee(EdgeSummaries), [][2]string{
		{"*ESE 32;FOO;*STB?", "36\n"},
		{"*ESR?;*STB?", "32;4\n"},
		{"*CLS;*STB?", "0\n"},
		{"*ESE 0;FOO;*ESE 255;FOO;*STB?", "4\n"},
		{"*SRE 4;*STB?", "68\n"},
		{"*CLS;*SRE 0;*ESE 255;FOO;*SRE 32;*STB?", "100\n"},
		{"*CLS;*OPC;*ESR?", "1\n"},
	})
	run(t, ieee(LevelSummaries), [][2]string{
		{"*ESE 0;FOO;*ESE 255;*STB?", "36\n"},
	})
	run(t, ieee(NoStatus), [][2]string{
		{"FOO;*OPC;*STB?;*ESR?;SYST:ERR?", "0;0;0,\"No error\"\n"},
	})
}

// line is a legacy dialect: C1/C2 prefixes, pairs, CHDR, inline errors.
func line() *Engine {
	d := Dialect{Name: "legacy", Grammar: Line, Sources: []Source{{"C", 1, 2}}, Status: NoStatus}
	d.Commands = append(d.Commands, Commons{IDN: func() string { return "M,X,S,1" }}.Commands("*IDN?", "*OPC?", "*STB?", "*ESR?", "*OPC")...)
	d.Commands = append(d.Commands, CommHeader()...)
	d.Commands = append(d.Commands, LegacyStatus("INR?")...)
	d.Commands = append(d.Commands,
		Command{"TDIV?", func(c *Call) error { c.Reply("TDIV", "1.00E-06s"); return nil }},
		Command{"C#:VDIV?", func(c *Call) error { c.Reply(fmt.Sprintf("C%d:VDIV", c.N(0)), "2.00E+00V"); return nil }},
		Command{"C#:VDIV", func(c *Call) error {
			if _, err := ParseLegacyNumber(c.Arg()); err != nil {
				return c.Error(ErrHeader)
			}
			return nil
		}},
		Command{"C#-C#:MEAD?", func(c *Call) error { c.Reply(fmt.Sprintf("C%d-C%d:MEAD", c.N(0), c.N(1)), c.Arg()); return nil }},
	)
	return New(d, nil)
}

// The legacy grammar: prefixes, separators, errors in place, CHDR.
// TRLC-LINKS: REQ-DSO-024
func TestLineMessages(t *testing.T) {
	run(t, line(), [][2]string{
		{"*IDN?", "M,X,S,1\n"},
		{"*OPC;*ESR?;*STB?;INR?", "*ESR 0\n*STB 0\nINR 0\n"},
		{" tdiv? \r\n", "TDIV 1.00E-06s\n"},
		{"c2:vdiv?", "C2:VDIV 2.00E+00V\n"},
		{"C1:VDIV 2V;C1:VDIV X", "Command header error\n"},
		{"C3:VDIV?", "Header suffix out of range\n"},
		{"C10:VDIV?", "Undefined header\n"},
		{"C1:", "Header separator error\n"},
		{"C1-C2:MEAD? frr", "C1-C2:MEAD FRR\n"},
		{"C1-C1:MEAD? FRR", "Header suffix out of range\n"},
		{"CX-C2:MEAD? FRR", "Header suffix out of range\n"},
		{"C1-C2:", "Undefined header\n"},
		{"FOO;;TDIV?", "Undefined header\nTDIV 1.00E-06s\n"},
		{"CHDR OFF;TDIV?;CHDR?", "1.00E-06s\nOFF\n"},
		{"CHDR LONG;CHDR?", "CHDR SHORT\n"},
		{"CHDR X", "Command header error\n"},
	})
}

// A dialect registered later replaces a shared command.
// TRLC-LINKS: REQ-DSO-024
func TestRegisterReplaces(t *testing.T) {
	e := line()
	e.Register(Command{"*IDN?", func(c *Call) error { c.Text("other"); return nil }})
	run(t, e, [][2]string{{"*IDN?", "other\n"}})
}

// Extensions answer their command and query forms; a refusal is -200.
// TRLC-LINKS: REQ-DSO-024
func TestExtensions(t *testing.T) {
	e := ieee(EdgeSummaries)
	state := "DONE"
	e.Register(Extensions([]Extension{{
		Header:  ":SYSTem:SELFCal",
		Command: func(string) error { state = "DOING"; return nil },
		Query:   func(string) ([]byte, error) { return []byte(state), nil },
	}, {
		Header:  ":SYSTem:REFuse",
		Command: func(string) error { return fmt.Errorf("no") },
	}})...)
	run(t, e, [][2]string{
		{":SYST:SELFC?;:SYST:SELFC;:SYST:SELFC?", "DONEDOING\n"},
		{":SYST:REF;:SYST:ERR?", "-200,\"Execution error\"\n"},
		{":SYST:REF?", ""},
		{":SYST:ERR?", "-113,\"Undefined header;:SYST:REF?\"\n"},
	})
}

// Legacy numbers take their unit suffixes.
// TRLC-LINKS: REQ-DSO-024
func TestParseLegacyNumber(t *testing.T) {
	for in, want := range map[string]float64{"1US": 1e-6, "500mv": .5, "2E-3": 2e-3, " 5 V": 5, "10NS": 1e-8, "3ms": 3e-3} {
		if v, err := ParseLegacyNumber(in); err != nil || v != want {
			t.Errorf("%q: %v %v", in, v, err)
		}
	}
	if _, err := ParseLegacyNumber("X"); err == nil {
		t.Error("X parsed")
	}
}

// Patterns: optional nodes, suffixes, choices.
// TRLC-LINKS: REQ-DSO-024
func TestPatterns(t *testing.T) {
	p := CompilePattern("SYSTem:ERRor[:NEXT]?")
	for _, h := range []string{"SYST:ERR?", ":SYSTEM:ERROR:NEXT?", "syst:err:next?"} {
		if _, ok := p.Match(h); !ok {
			t.Errorf("no match %s", h)
		}
	}
	if _, ok := p.Match("SYST:ERRO?"); ok {
		t.Error("partial form matched")
	}
	p = CompilePattern(":MEASure:ADVanced:P#:SOURce1")
	if s, ok := p.Match(":MEAS:ADV:P3:SOUR1"); !ok || s[0] != 3 {
		t.Errorf("suffix %v %v", s, ok)
	}
	if !MatchChoice("ONEMeg", "onem") || !MatchChoice("FIFTy", "FIFTY") || MatchChoice("FIFTy", "FIF") {
		t.Error("choice")
	}
	for s, m := range map[string]float64{"MV": 1e-3, "MA": 1e6, "US": 1e-6, "KHZ": 1e3, "MHZ": 1e6, "k": 1e3, "V": 1} {
		if got, _, ok := ResolveUnit(s); !ok || got != m {
			t.Errorf("unit %s: %v %v", s, got, ok)
		}
	}
}

// Fuzzed messages never panic in either grammar.
// TRLC-LINKS: REQ-DSO-024
func FuzzExec(f *testing.F) {
	for _, s := range []string{"*IDN?", ":CHAN1:SCAL 1;OFFS?", "C1-C2:MEAD? FRR", "#9", "'a;b", "C1:VDIV 1V;C", ":FORM:DATA CUST,0"} {
		f.Add(s)
	}
	a, b := ieee(EdgeSummaries), line()
	f.Fuzz(func(t *testing.T, s string) {
		a.Exec([]byte(s))
		b.Exec([]byte(s))
		_ = strings.ToUpper(s)
	})
}
