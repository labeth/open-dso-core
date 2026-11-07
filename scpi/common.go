package scpi

import (
	"strconv"
	"strings"
)

// Commons supplies the instrument-specific parts of the IEEE 488.2 common
// commands. A nil hook leaves its command out.
type Commons struct {
	IDN    func() string              // *IDN? reply: four comma-separated fields
	Reset  func(c *Call) error        // *RST
	Option func() string              // *OPT? reply
	Test   func() int                 // *TST? result, 0 = pass; nil answers 0
	Save   func(c *Call, n int) error // *SAV <n>
	Recall func(c *Call, n int) error // *RCL <n>
	Wait   func(c *Call)              // *WAI and *OPC?: until pending operations are done
}

// Commands are the common commands named (all when none is named): *IDN?,
// *RST, *CLS, *ESE, *ESE?, *SRE, *SRE?, *ESR?, *STB?, *OPC, *OPC?, *WAI,
// *TST?, *OPT?, *SAV, *RCL.
// TRLC-LINKS: REQ-DSO-024
func (h Commons) Commands(names ...string) []Command {
	all := []Command{
		{"*IDN?", func(c *Call) error { c.Text(h.IDN()); return nil }},
		{"*RST", func(c *Call) error { return h.Reset(c) }},
		{"*CLS", func(c *Call) error {
			s := &c.e.st
			c.e.errs.clear()
			s.setESR(0)
			s.setEvent(&s.oper, STBOPER, 0)
			s.setEvent(&s.ques, STBQUES, 0)
			return nil
		}},
		{"*ESE", func(c *Call) error { return setMask(c, func(v uint16) { c.e.st.ese = v }) }},
		{"*ESE?", func(c *Call) error { c.Reply("*ESE", strconv.Itoa(int(c.e.st.ese))); return nil }},
		{"*SRE", func(c *Call) error { return setMask(c, c.e.st.setSRE) }},
		{"*SRE?", func(c *Call) error { c.Reply("*SRE", strconv.Itoa(int(c.e.st.sre))); return nil }},
		{"*ESR?", func(c *Call) error {
			v := c.e.st.esr
			c.e.st.setESR(0)
			c.Reply("*ESR", strconv.Itoa(int(v)))
			return nil
		}},
		{"*STB?", func(c *Call) error {
			c.Reply("*STB", strconv.Itoa(int(c.e.st.byte(len(c.e.errs.q) > 0))))
			return nil
		}},
		{"*OPC", func(c *Call) error { c.e.st.setESR(c.e.st.esr | ESROPC); return nil }},
		{"*OPC?", func(c *Call) error {
			if h.Wait != nil {
				h.Wait(c)
			}
			c.Text("1")
			return nil
		}},
		{"*WAI", func(c *Call) error {
			if h.Wait != nil {
				h.Wait(c)
			}
			return nil
		}},
		{"*TST?", func(c *Call) error {
			r := 0
			if h.Test != nil {
				r = h.Test()
			}
			c.Reply("*TST", strconv.Itoa(r))
			return nil
		}},
		{"*OPT?", func(c *Call) error { c.Text(h.Option()); return nil }},
		{"*SAV", func(c *Call) error { return slot(c, h.Save) }},
		{"*RCL", func(c *Call) error { return slot(c, h.Recall) }},
	}
	missing := map[string]bool{
		"*IDN?": h.IDN == nil, "*RST": h.Reset == nil, "*OPT?": h.Option == nil,
		"*SAV": h.Save == nil, "*RCL": h.Recall == nil,
	}
	var out []Command
	for _, cmd := range all {
		if missing[cmd.Pattern] {
			continue
		}
		if len(names) == 0 {
			out = append(out, cmd)
			continue
		}
		for _, n := range names {
			if n == cmd.Pattern {
				out = append(out, cmd)
			}
		}
	}
	return out
}

// setMask reads *ESE/*SRE's value: 16 bits, no range check; missing is
// -109. A Line-grammar mask is accepted without a value check.
// TRLC-LINKS: REQ-DSO-024
func setMask(c *Call, set func(uint16)) error {
	if c.e.d.Grammar == Line {
		if v, err := strconv.Atoi(c.Arg()); err == nil {
			set(uint16(v))
		}
		return nil
	}
	v, _, err := c.Int(true)
	if err != nil {
		return err
	}
	set(uint16(v))
	return nil
}

// slot reads *SAV/*RCL's slot number and runs the hook.
// TRLC-LINKS: REQ-DSO-024
func slot(c *Call, run func(*Call, int) error) error {
	if c.e.d.Grammar == Line {
		n, err := strconv.Atoi(c.Arg())
		if err != nil {
			return c.Error(ErrHeader)
		}
		return run(c, n)
	}
	n, _, err := c.Int(true)
	if err != nil {
		return err
	}
	return run(c, int(n))
}

// System is the SCPI-99 SYSTem/STATus subset: SYSTem:ERRor[:NEXT]?,
// SYSTem:COUNt? (the queued entries, the overflow marker counted),
// SYSTem:VERSion? and the OPERation/QUEStionable registers.
// TRLC-LINKS: REQ-DSO-024
func System() []Command {
	cmds := []Command{
		{"SYSTem:ERRor[:NEXT]?", func(c *Call) error {
			e := c.e.errs.pop()
			c.Text(e.format(c.e.d.Errors))
			return nil
		}},
		{"SYSTem:COUNt?", func(c *Call) error { c.NR1(int64(len(c.e.errs.q))); return nil }},
		{"SYSTem:VERSion?", func(c *Call) error { c.Text("1999.0"); return nil }},
		{"STATus:PRESet", func(c *Call) error {
			c.e.st.oper.enable, c.e.st.ques.enable = 0, 0
			return nil
		}},
	}
	reg := func(root string, bit uint8, r func(*Call) *eventReg) {
		cmds = append(cmds,
			Command{root + "[:EVENt]?", func(c *Call) error {
				reg := r(c)
				v := reg.event
				c.e.st.setEvent(reg, bit, 0)
				c.NR1(int64(v))
				return nil
			}},
			Command{root + ":CONDition?", func(c *Call) error { c.NR1(int64(r(c).cond)); return nil }},
			Command{root + ":ENABle", func(c *Call) error {
				v, _, err := c.Int(true)
				if err == nil {
					r(c).enable = uint16(v)
				}
				return err
			}},
			Command{root + ":ENABle?", func(c *Call) error { c.NR1(int64(r(c).enable)); return nil }})
	}
	reg("STATus:OPERation", STBOPER, func(c *Call) *eventReg { return &c.e.st.oper })
	reg("STATus:QUEStionable", STBQUES, func(c *Call) *eventReg { return &c.e.st.ques })
	return cmds
}

// LegacyStatus are the legacy status queries named, each answering 0 under
// its header (no legacy register has a bit source): INR?, CMR?, DDR?, EXR?,
// URR?, and ALST? with the status byte and ESR.
// TRLC-LINKS: REQ-DSO-024
func LegacyStatus(names ...string) []Command {
	zero := func(h string) Command {
		return Command{h + "?", func(c *Call) error { c.Reply(h, "0"); return nil }}
	}
	all := map[string]Command{
		"INR?": zero("INR"), "CMR?": zero("CMR"), "DDR?": zero("DDR"), "EXR?": zero("EXR"), "URR?": zero("URR"),
		"ALST?": {"ALST?", func(c *Call) error {
			stb := c.e.st.byte(len(c.e.errs.q) > 0)
			c.Reply("ALST", "STB,"+strconv.Itoa(int(stb))+",ESR,"+strconv.Itoa(int(c.e.st.esr))+
				",INR,0,DDR,0,CMR,0,EXR,0,URR,0")
			return nil
		}},
	}
	var out []Command
	for _, n := range names {
		if c, ok := all[n]; ok {
			out = append(out, c)
		}
	}
	return out
}

// CommHeader is the legacy CHDR command pair. CHDR SHORT, LONG and OFF set
// the mode (ON is SHORT; LONG is SHORT when the dialect has no long
// headers); anything else is bad, which the Line grammar answers as a
// header error and the IEEE grammar as an illegal value. CHDR? answers
// under CHDR in SHORT, COMM_HEADER in LONG, and the bare mode in OFF.
// TRLC-LINKS: REQ-DSO-024
func CommHeader() []Command {
	return []Command{
		{"CHDR", func(c *Call) error {
			var v string
			if c.e.d.Grammar == Line {
				v = c.Arg()
			} else {
				t, ok, err := c.Param(false)
				if err != nil {
					return err
				}
				if !ok {
					return ErrFailed
				}
				v = strings.ToUpper(t.Str)
			}
			switch v {
			case "OFF":
				c.e.hdr = HeaderOff
			case "SHORT", "ON":
				c.e.hdr = HeaderShort
			case "LONG":
				c.e.hdr = HeaderShort
				if c.e.d.Header.Long {
					c.e.hdr = HeaderLong
				}
			default:
				if c.e.d.Grammar == Line {
					return c.Error(ErrHeader)
				}
				return c.Error(ErrIllegalParamValue)
			}
			return nil
		}},
		{"CHDR?", func(c *Call) error {
			v := [...]string{"SHORT", "LONG", "OFF"}[c.e.hdr]
			c.Legacy("CHDR", "COMM_HEADER", v, "")
			return nil
		}},
	}
}
