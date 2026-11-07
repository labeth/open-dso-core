package prototrig

import (
	"reflect"
	"slices"
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// TRLC-LINKS: REQ-DSO-1001
func TestConfigurationsAreProtocolTriggers(t *testing.T) {
	for want, p := range map[string]hal.ProtoTrigger{
		"i2c": I2C{}, "spi": SPI{}, "uart": UART{}, "can": CAN{}, "lin": LIN{},
	} {
		if p.Protocol() != want {
			t.Errorf("%T names %q, want %q", p, p.Protocol(), want)
		}
	}
}

// Equal must look at every field: a field left out would make a board
// that keeps the request it planned take a changed request for the one it
// has staged.
// TRLC-LINKS: REQ-DSO-013
func TestSerialParamsEqualSeesEveryField(t *testing.T) {
	base := SerialParams{Bytes: []int{1}, Pattern: []PatternElem{{Kind: 2}}}
	v := reflect.ValueOf(&base).Elem()
	for i := range v.NumField() {
		s := base
		s.Bytes, s.Pattern = slices.Clone(base.Bytes), slices.Clone(base.Pattern)
		f := reflect.ValueOf(&s).Elem().Field(i)
		change, ok := map[reflect.Kind]func(){
			reflect.Bool:    func() { f.SetBool(!f.Bool()) },
			reflect.Float64: func() { f.SetFloat(f.Float() + 1) },
			reflect.Int:     func() { f.SetInt(f.Int() + 1) },
			reflect.String:  func() { f.SetString(f.String() + "x") },
			reflect.Slice:   func() { f.Index(0).Set(reflect.Zero(f.Index(0).Type())); f.Set(reflect.Append(f, f.Index(0))) },
		}[f.Kind()]
		if !ok {
			t.Fatalf("field %s: kind %v not covered by the test", v.Type().Field(i).Name, f.Kind())
		}
		change()
		if s.Equal(&base) || base.Equal(&s) {
			t.Errorf("field %s: a change is not seen", v.Type().Field(i).Name)
		}
	}
	if (&SerialParams{}).Equal(&SerialParams{Bytes: []int{}}) {
		t.Error("nil and empty bytes are equal")
	}
	b := Bus{Request: base, ARINCSlicer: []float64{1, 2, 3, 4}}
	o := b
	if !b.Equal(&o) {
		t.Error("a copy differs")
	}
	o.Levels[1] = 1
	if b.Equal(&o) {
		t.Error("a level is not seen")
	}
	o = b
	o.ARINCSlicer = []float64{1, 2, 3, 5}
	if b.Equal(&o) {
		t.Error("the slicer is not seen")
	}
}
