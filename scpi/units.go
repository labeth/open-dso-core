package scpi

import "strings"

// baseUnits are the unit names accepted after a number (the libscpi unit
// table: V, S, HZ, PCT, DB*, ...).
var baseUnits = []string{
	"SA/S", "S/S", "PTS", "DBUV", "DBUA", "DBMV", "DBM", "DBV", "DB", "OHM", "PCT", "DIV",
	"HZ", "BIT", "CEL", "DEG", "V", "A", "W", "S", "%",
}

// prefixes are the SCPI-99 multiplier mnemonics. "M" is milli; mega is "MA".
var prefixes = []struct {
	p string
	m float64
}{
	{"EX", 1e18}, {"PE", 1e15}, {"MA", 1e6}, {"T", 1e12}, {"G", 1e9}, {"K", 1e3},
	{"M", 1e-3}, {"U", 1e-6}, {"N", 1e-9}, {"P", 1e-12}, {"F", 1e-15}, {"A", 1e-18},
}

// specialUnits are complete suffixes with a fixed meaning (SCPI convention: MHZ and MOHM are mega).
var specialUnits = map[string]struct {
	unit string
	mult float64
}{
	"MHZ":   {"HZ", 1e6},
	"MOHM":  {"OHM", 1e6},
	"MS/S":  {"SA/S", 1e6},
	"MSA/S": {"SA/S", 1e6},
	"GS/S":  {"SA/S", 1e9},
	"GSA/S": {"SA/S", 1e9},
	"KS/S":  {"SA/S", 1e3},
	"KSA/S": {"SA/S", 1e3},
	"PCT":   {"PCT", 1},
	"%":     {"PCT", 1},
}

// ResolveUnit splits a suffix into multiplier and base unit. ok=false for an unknown suffix.
// A bare multiplier ("1m", "10k", "2MA") is accepted with base unit "".
// TRLC-LINKS: REQ-DSO-024
func ResolveUnit(suffix string) (mult float64, base string, ok bool) {
	s := strings.ToUpper(suffix)
	if s == "" {
		return 1, "", true
	}
	if su, ok := specialUnits[s]; ok {
		return su.mult, su.unit, true
	}
	for _, b := range baseUnits {
		if s == b {
			return 1, b, true
		}
	}
	for _, p := range prefixes {
		if !strings.HasPrefix(s, p.p) {
			continue
		}
		rest := s[len(p.p):]
		if rest == "" {
			return p.m, "", true
		}
		for _, b := range baseUnits {
			if rest == b {
				return p.m, b, true
			}
		}
	}
	return 0, "", false
}
