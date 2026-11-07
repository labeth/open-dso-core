package scpi

import (
	"strconv"
	"strings"
)

// legacySuffixes are the unit suffixes the legacy short forms take after a
// number, multiplier included ("1US", "500MV").
var legacySuffixes = []struct {
	s string
	m float64
}{
	{"NS", 1e-9}, {"US", 1e-6}, {"MS", 1e-3}, {"MV", 1e-3}, {"UV", 1e-6}, {"V", 1}, {"S", 1},
}

// ParseLegacyNumber parses a legacy short-form number with an optional unit
// suffix (NS, US, MS, MV, UV, V, S; case-insensitive).
// TRLC-LINKS: REQ-DSO-024
func ParseLegacyNumber(s string) (float64, error) {
	up := strings.ToUpper(strings.TrimSpace(s))
	mult := 1.0
	for _, suf := range legacySuffixes {
		if before, ok := strings.CutSuffix(up, suf.s); ok {
			up, mult = before, suf.m
			break
		}
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(up), 64)
	return v * mult, err
}

// ParseOnOff reads a legacy ON/OFF argument.
// TRLC-LINKS: REQ-DSO-024
func ParseOnOff(s string) (on, ok bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ON":
		return true, true
	case "OFF":
		return false, true
	}
	return false, false
}
