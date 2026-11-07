// Package profile reads a scope's profile.yaml into a hal.Profile.
//
// A profile file is a base (the hal.Profile fields, by their yaml names)
// plus a list of variants. Each variant matches on the unit's identity and
// overlays the base with its own settings:
//
//	channels: 2
//	format: {bits: 8, storage: u8, min: 0, max: 255, mid: 128}
//	variants:
//	  - name: sds1102cml-plus
//	    match: {model: SDS1102CML+}
//	    set:
//	      bandwidth: 100e6
//
// Every variant whose match holds is applied, in file order. Two applied
// variants that set the same setting to different values are a load error,
// not a silent last-one-wins: which one is right is a decision for the
// profile's author. An identity that no variant matches is refused (a
// *hal.Refusal) so that the core never runs a unit nobody described.
//
// Matching: vendor, model, serial, hw_revision, adc_part and vendor_firmware
// take a path.Match pattern; licences lists licences the unit must hold;
// fitted maps a census part to a pattern its verdict must match.
//
// The file is read by a parser of the YAML subset a profile needs (see
// yaml.go) so the app module takes no YAML dependency.
package profile

import (
	"errors"
	"fmt"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"

	"github.com/labeth/open-dso-core/hal"
)

// Doc is a parsed profile file.
type Doc struct {
	base     *node
	variants []variant
}

type variant struct {
	name  string
	match match
	set   *node
}

type match struct {
	fields   map[string]string // identity field name -> pattern
	licences []string
	fitted   map[string]string
}

// Load parses data and resolves it for id.
func Load(data []byte, id hal.Identity) (hal.Profile, error) {
	d, err := Parse(data)
	if err != nil {
		return hal.Profile{}, err
	}
	return d.Resolve(id)
}

// Parse reads a profile file and checks every part of it against the
// schema, so that a mistake in a variant shows on any unit, not only on
// the one it matches.
func Parse(data []byte) (*Doc, error) {
	root, err := parseYAML(data)
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	if root.kind != mapNode {
		return nil, errors.New("profile: the file must be a mapping")
	}
	d := &Doc{base: &node{kind: mapNode, m: map[string]*node{}}}
	for _, k := range root.keys {
		if k == "variants" {
			continue
		}
		d.base.keys = append(d.base.keys, k)
		d.base.m[k] = root.m[k]
	}
	var scratch hal.Profile
	if err := decode(d.base, reflect.ValueOf(&scratch).Elem(), ""); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	vs, ok := root.m["variants"]
	if !ok || vs.kind != seqNode || len(vs.items) == 0 {
		return nil, errors.New("profile: no variants: every unit must match one")
	}
	for i, it := range vs.items {
		v, err := parseVariant(it, i)
		if err != nil {
			return nil, fmt.Errorf("profile: %w", err)
		}
		if slices.ContainsFunc(d.variants, func(o variant) bool { return o.name == v.name }) {
			return nil, fmt.Errorf("profile: variant %q twice", v.name)
		}
		var scratch hal.Profile
		if err := decode(v.set, reflect.ValueOf(&scratch).Elem(), "variants."+v.name+".set"); err != nil {
			return nil, fmt.Errorf("profile: %w", err)
		}
		d.variants = append(d.variants, v)
	}
	return d, nil
}

// Variants returns the variant names in file order.
func (d *Doc) Variants() []string {
	out := make([]string, len(d.variants))
	for i, v := range d.variants {
		out[i] = v.name
	}
	return out
}

// Resolve returns the profile for id: the base overlaid by every matching
// variant, in order, with Identity set to id.
func (d *Doc) Resolve(id hal.Identity) (hal.Profile, error) {
	var applied []variant
	for _, v := range d.variants {
		if v.match.holds(id) {
			applied = append(applied, v)
		}
	}
	if len(applied) == 0 {
		return hal.Profile{}, &hal.Refusal{
			Feature: "identity",
			Reason:  fmt.Sprintf("no profile variant matches this unit (vendor %q, model %q, hardware revision %q)", id.Vendor, id.Model, id.HWRevision),
		}
	}
	if err := checkConflicts(applied); err != nil {
		return hal.Profile{}, err
	}
	merged := d.base
	for _, v := range applied {
		merged = mergeNodes(merged, v.set)
	}
	var p hal.Profile
	if err := decode(merged, reflect.ValueOf(&p).Elem(), ""); err != nil {
		return hal.Profile{}, fmt.Errorf("profile: %w", err)
	}
	p.Identity = id
	if err := Validate(p); err != nil {
		return hal.Profile{}, fmt.Errorf("profile for %s: %w", names(applied), err)
	}
	return p, nil
}

func names(vs []variant) string {
	n := make([]string, len(vs))
	for i, v := range vs {
		n[i] = v.name
	}
	return strings.Join(n, "+")
}

// Validate reports a resolved profile the core cannot run on.
func Validate(p hal.Profile) error {
	switch {
	case p.Channels < 1:
		return errors.New("channels must be at least 1")
	case len(p.Rates) == 0:
		return errors.New("no rates")
	case p.MaxDepth < 1:
		return errors.New("max_depth must be at least 1")
	case p.Content == 0:
		return errors.New("content is empty: readout delivers nothing")
	case p.Display.W < 1 || p.Display.H < 1 || p.Display.Format == 0:
		return errors.New("display needs w, h and format")
	case p.Stage != 1 && p.Stage != 2:
		return fmt.Errorf("stage %d: want 1 (vendor bitstream) or 2 (open bitstream)", p.Stage)
	}
	if err := p.Format.Validate(); err != nil {
		return err
	}
	for i, r := range p.Rates {
		if r.SamplesPerS <= 0 || r.MaxChannels < 1 || r.MaxChannels > p.Channels {
			return fmt.Errorf("rates[%d]: want a positive rate for 1..%d channels", i, p.Channels)
		}
	}
	for i, t := range p.Timebase.TdivS {
		if t <= 0 || (i > 0 && t <= p.Timebase.TdivS[i-1]) {
			return fmt.Errorf("timebase.tdiv_s[%d]: want positive and ascending", i)
		}
	}
	for _, part := range slices.Sorted(maps.Keys(p.Absent)) {
		if !slices.Contains(hal.OptionalParts, part) {
			return fmt.Errorf("absent.%s: not an optional part (%s)", part, strings.Join(hal.OptionalParts, ", "))
		}
	}
	return nil
}

func parseVariant(n *node, i int) (variant, error) {
	where := fmt.Sprintf("variants[%d]", i)
	if n.kind != mapNode {
		return variant{}, mismatch(n, where, "a mapping")
	}
	var v variant
	for _, k := range n.keys {
		val := n.m[k]
		switch k {
		case "name":
			if val.kind != scalarNode || val.value == "" {
				return variant{}, mismatch(val, where+".name", "a name")
			}
			v.name = val.value
		case "match":
			m, err := parseMatch(val, where+".match")
			if err != nil {
				return variant{}, err
			}
			v.match = m
		case "set":
			if val.kind != mapNode && val.kind != nullNode {
				return variant{}, mismatch(val, where+".set", "a mapping")
			}
			v.set = val
		default:
			return variant{}, fmt.Errorf("%s.%s (line %d): unknown key", where, k, val.line)
		}
	}
	switch {
	case v.name == "":
		return variant{}, fmt.Errorf("%s (line %d): no name", where, n.line)
	case len(v.match.fields) == 0 && len(v.match.licences) == 0 && len(v.match.fitted) == 0:
		return variant{}, fmt.Errorf("variant %q (line %d): no match: say which units it is for", v.name, n.line)
	}
	if v.set == nil || v.set.kind == nullNode {
		v.set = &node{kind: mapNode, m: map[string]*node{}}
	}
	return v, nil
}

var matchFields = []string{"vendor", "model", "serial", "hw_revision", "adc_part", "vendor_firmware"}

func parseMatch(n *node, where string) (match, error) {
	if n.kind != mapNode {
		return match{}, mismatch(n, where, "a mapping")
	}
	m := match{fields: map[string]string{}}
	for _, k := range n.keys {
		val := n.m[k]
		switch {
		case slices.Contains(matchFields, k):
			if val.kind != scalarNode {
				return match{}, mismatch(val, where+"."+k, "a pattern")
			}
			if _, err := path.Match(val.value, ""); err != nil {
				return match{}, fmt.Errorf("%s.%s (line %d): %w", where, k, val.line, err)
			}
			m.fields[k] = val.value
		case k == "licences":
			if err := decode(val, reflect.ValueOf(&m.licences).Elem(), where+".licences"); err != nil {
				return match{}, err
			}
		case k == "fitted":
			if err := decode(val, reflect.ValueOf(&m.fitted).Elem(), where+".fitted"); err != nil {
				return match{}, err
			}
		default:
			return match{}, fmt.Errorf("%s.%s (line %d): unknown identity field", where, k, val.line)
		}
	}
	return m, nil
}

func (m match) holds(id hal.Identity) bool {
	got := map[string]string{
		"vendor": id.Vendor, "model": id.Model, "serial": id.Serial,
		"hw_revision": id.HWRevision, "adc_part": id.ADCPart, "vendor_firmware": id.VendorFirmware,
	}
	for k, pat := range m.fields {
		if ok, _ := path.Match(pat, got[k]); !ok {
			return false
		}
	}
	for _, l := range m.licences {
		if !slices.Contains(id.Licences, l) {
			return false
		}
	}
	for part, pat := range m.fitted {
		v, present := id.Fitted[part]
		if ok, _ := path.Match(pat, v); !present || !ok {
			return false
		}
	}
	return true
}

// leaf is one setting a variant makes: a scalar, a list or an empty mapping.
type leaf struct {
	path []string
	n    *node
}

func flatten(n *node, prefix []string, out []leaf) []leaf {
	if n.kind != mapNode || len(n.keys) == 0 {
		return append(out, leaf{path: slices.Clone(prefix), n: n})
	}
	for _, k := range n.keys {
		out = flatten(n.m[k], append(prefix, k), out)
	}
	return out
}

// checkConflicts refuses two applied variants that set one setting to
// different values, or one as a mapping and the other inside it.
func checkConflicts(vs []variant) error {
	sets := make([][]leaf, len(vs))
	for i, v := range vs {
		if len(v.set.keys) > 0 {
			sets[i] = flatten(v.set, nil, nil)
		}
	}
	for i := range vs {
		for j := i + 1; j < len(vs); j++ {
			for _, a := range sets[i] {
				for _, b := range sets[j] {
					if err := conflict(vs[i].name, vs[j].name, a, b); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func conflict(an, bn string, a, b leaf) error {
	short, long := a.path, b.path
	if len(short) > len(long) {
		short, long = long, short
	}
	if !slices.Equal(short, long[:len(short)]) {
		return nil
	}
	if len(a.path) == len(b.path) && a.n.equal(b.n) {
		return nil
	}
	return fmt.Errorf("profile: variants %q (line %d) and %q (line %d) both set %s: the profile must say which applies",
		an, a.n.line, bn, b.n.line, strings.Join(short, "."))
}

// mergeNodes returns base overlaid with over: mappings merge key by key,
// anything else in over replaces what base has. Neither input is changed.
func mergeNodes(base, over *node) *node {
	if base.kind != mapNode || over.kind != mapNode {
		return over
	}
	out := &node{kind: mapNode, m: make(map[string]*node, len(base.keys)), keys: slices.Clone(base.keys), line: base.line}
	maps.Copy(out.m, base.m)
	for _, k := range over.keys {
		if b, ok := out.m[k]; ok {
			out.m[k] = mergeNodes(b, over.m[k])
			continue
		}
		out.keys = append(out.keys, k)
		out.m[k] = over.m[k]
	}
	return out
}
