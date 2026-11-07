package scpi

// ESR bits (IEEE 488.2 standard event status register).
const (
	ESROPC = 1 << 0
	ESRRQC = 1 << 1
	ESRQYE = 1 << 2
	ESRDDE = 1 << 3
	ESREXE = 1 << 4
	ESRCME = 1 << 5
	ESRURQ = 1 << 6
	ESRPON = 1 << 7
)

// STB bits.
const (
	STBEAV  = 1 << 2 // error queue not empty
	STBQUES = 1 << 3
	STBMAV  = 1 << 4
	STBESB  = 1 << 5
	STBMSS  = 1 << 6
	STBOPER = 1 << 7
)

// Summaries selects how the status byte's summary bits follow their
// registers.
type Summaries int

// The summary rules.
const (
	// EdgeSummaries recomputes a summary bit only when its event register changes
	// value: writing an enable mask does not, so *ESE after an error leaves
	// ESB as it was (libscpi). MSS follows every change of STB or SRE.
	EdgeSummaries Summaries = iota
	// LevelSummaries computes every summary from the registers when STB is read
	// (IEEE 488.2).
	LevelSummaries
	// NoStatus is no status model: *OPC and errors leave every register 0.
	NoStatus
)

// eventReg is a 16-bit condition/event/enable triple (OPERation,
// QUEStionable).
type eventReg struct{ cond, event, enable uint16 }

// status is the IEEE 488.2 / SCPI status model.
type status struct {
	rule          Summaries
	esr, ese, sre uint16
	stb           uint8 // Edge: the latched summary bits (ESB, QUES, OPER, MSS)
	oper, ques    eventReg
}

// esrBit maps an error code to the ESR bit of its class.
// TRLC-LINKS: REQ-DSO-024
func esrBit(code int) uint16 {
	switch {
	case code > 0:
		return ESRDDE
	case code <= -100 && code > -200:
		return ESRCME
	case code <= -200 && code > -300:
		return ESREXE
	case code <= -300 && code > -400:
		return ESRDDE
	case code <= -400 && code > -500:
		return ESRQYE
	case code <= -500 && code > -600:
		return ESRPON
	case code <= -600 && code > -700:
		return ESRURQ
	case code <= -700 && code > -800:
		return ESRRQC
	case code <= -800 && code > -900:
		return ESROPC
	}
	return 0
}

// TRLC-LINKS: REQ-DSO-024
func setBit(b *uint8, bit uint8, on bool) {
	if on {
		*b |= bit
	} else {
		*b &^= bit
	}
}

// setESR writes ESR; in the Edge rule ESB follows only a change of value.
// TRLC-LINKS: REQ-DSO-024
func (s *status) setESR(v uint16) {
	if s.rule == NoStatus || v == s.esr {
		return
	}
	s.esr = v
	setBit(&s.stb, STBESB, v&s.ese != 0)
	s.mss()
}

// setEvent writes an event register; its summary follows a change of value.
// TRLC-LINKS: REQ-DSO-024
func (s *status) setEvent(r *eventReg, bit uint8, v uint16) {
	if s.rule == NoStatus || v == r.event {
		return
	}
	r.event = v
	setBit(&s.stb, bit, v&r.enable != 0)
	s.mss()
}

// setSRE writes the service-request enable mask.
// TRLC-LINKS: REQ-DSO-024
func (s *status) setSRE(v uint16) {
	if s.rule == NoStatus {
		return
	}
	s.sre = v
	s.mss()
}

// mss recomputes the master summary of the latched byte.
// TRLC-LINKS: REQ-DSO-024
func (s *status) mss() {
	setBit(&s.stb, STBMSS, s.stb&uint8(s.sre)&^STBMSS != 0)
}

// byte is the status byte; errs reports a non-empty error queue.
// TRLC-LINKS: REQ-DSO-024
func (s *status) byte(errs bool) uint8 {
	switch s.rule {
	case NoStatus:
		return 0
	case LevelSummaries:
		var b uint8
		setBit(&b, STBEAV, errs)
		setBit(&b, STBQUES, s.ques.event&s.ques.enable != 0)
		setBit(&b, STBESB, s.esr&s.ese != 0)
		setBit(&b, STBOPER, s.oper.event&s.oper.enable != 0)
		setBit(&b, STBMSS, b&uint8(s.sre)&^STBMSS != 0)
		return b
	}
	b := s.stb &^ STBEAV
	setBit(&b, STBEAV, errs)
	setBit(&b, STBMSS, b&uint8(s.sre)&^STBMSS != 0)
	return b
}
