// ENGMODEL-OWNER-UNIT: FU-DSO-ENGINE

package engine

import (
	"github.com/labeth/open-dso-core/hal"
	"github.com/labeth/open-dso-core/instrument"
	"github.com/labeth/open-dso-core/sample"
)

// Software never decides a trigger (ADR-SERIAL-HARDWARE-TRIGGER); on a
// board whose hardware edge trigger names its place only to a few samples
// (Profile.TriggerIndex) it checks the trigger the board pointed out on
// the record and places it on the level's crossing: an index with no
// crossing of the level in the trigger's direction near it (a comparator
// on raw codes firing on converter noise at a small signal's rail) is no
// trigger, shown untriggered in AUTO and waited past in NORMAL, and
// counted. A decimated record whose averages hide the crossing keeps the
// board's index. An envelope is checked, not placed: its buckets hold
// extremes, not samples in time order.

// placeEdge checks and places frame f's hardware edge trigger; false: the
// record has no crossing at the index, and f is now untriggered. whole: a
// stopped record read whole, whose capture was shown already: placed,
// never dropped.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-016
func (c *HALCycle) placeEdge(f *sample.Frame, whole bool) bool {
	if c.b.Profile().TriggerIndex <= 1 || c.cur.SerialMode != 0 || c.cur.Trigger.Type != instrument.TrigEdge ||
		!f.Status.Triggered || f.TriggerAt < 0 || f.TrigKind != sample.HardwareNode {
		return true
	}
	spec := c.cur.TriggerSpec()
	if len(spec.Stages) == 0 || spec.Stages[0].Node != hal.NodeEdge {
		return true
	}
	st := spec.Stages[0]
	var t *sample.Trace
	for i := range f.Traces {
		if f.Traces[i].Ch == st.Source.Index {
			t = &f.Traces[i]
		}
	}
	if t == nil || t.Valid < 2 || !(t.Scale.PerCode > 0) {
		return true
	}
	n := t.Valid
	disc := make([]uint8, n)
	q := t.Precision
	if len(q) < n {
		q = make([]uint16, n)
		for k := range n {
			q[k] = t.Format.Q88(t.Word(k))
		}
	}
	for k := range n {
		disc[k] = t.Format.Code8(t.Word(k))
	}
	level := 128 + (t.Scale.Word(st.Levels.High)-float64(t.Format.Mid))/t.Format.Unit8()
	rising := st.Levels.Slope != hal.Falling
	envelope := f.Content == sample.PeakPairs || f.Flags.Has(sample.Envelope)
	confirmed := ConfirmTriggerEdge(disc, q, n, f.TriggerAt, level, rising, envelope)
	if !confirmed && f.AcqDecim > 1 {
		c.inst.NoteTrigger(false)
		return true
	}
	if !whole {
		c.inst.NoteTrigger(!confirmed)
	}
	if confirmed && !envelope {
		f.TriggerAt = RefineTriggerEdge(disc, q, n, f.TriggerAt, level, rising)
	}
	if confirmed || whole {
		return true
	}
	f.Status.Triggered = false
	f.TrigKind, f.TrigNode = sample.Forced, ""
	f.TriggerAt = -1
	return false
}
