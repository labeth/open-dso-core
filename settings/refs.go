// ENGMODEL-OWNER-UNIT: FU-DSO-SETTINGS

package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/labeth/open-dso-core/hal"
)

// RefsKey is the stored references' key in a scope's settings store: a
// store of their own beside the setup file, not in it. A reference is
// screen columns, kilobytes a channel, and changes only when one is stored
// or cleared, while the setup file is rewritten on every settled knob; and
// a recalled setup never brings back the references stored with it.
const RefsKey = "references.json"

// RefsVersion is the stored references' format version.
const RefsVersion = 1

// Refs are the reference slots (REF A, REF B) as stored.
type Refs struct {
	Version int        `json:"version"`
	Slots   [2]RefSlot `json:"slots"`
}

// RefSlot is one stored reference: each channel's screen columns (8-bit
// display codes, nil for a channel it did not hold) and what they were
// stored at, so they redraw at the same volts and the same time.
type RefSlot struct {
	Has     bool      `json:"has"`
	Show    bool      `json:"show"`
	Cols    [][]byte  `json:"cols,omitempty"`  // per channel, C1 first
	VdivV   []float64 `json:"vdiv_v"`          // each channel's analog V/div at the store
	OffV    []float64 `json:"off_v"`           // each channel's analog offset
	Probe   []float64 `json:"probe"`           // each channel's probe factor
	CPD     []float64 `json:"cpd"`             // each channel's codes a division (0: an 8-bit converter's)
	TdivS   float64   `json:"tdiv_s"`          // the timebase it lines up at
	PosFrac float64   `json:"pos_frac"`        // the trigger's place across the screen then
	At      int64     `json:"at_ms,omitempty"` // when it was stored (Unix ms)
	// Setup is the setup it was stored at, for AUTO in COMPARE (nil: none).
	Setup *RefSetupSaved `json:"setup,omitempty"`
}

// RefSetupSaved is the setup a reference was stored at: the timebase and
// trigger position, each channel's V/div and requested offset, and the
// trigger.
type RefSetupSaved struct {
	TdivS     float64   `json:"tdiv_s"`
	PosFrac   float64   `json:"pos_frac"`
	VdivV     []float64 `json:"vdiv_v"`  // each channel's V/div at the BNC
	RangeV    []float64 `json:"range_v"` // each channel's analog range a division
	OffReqV   []float64 `json:"off_req_v"`
	TrigLevel uint64    `json:"trig_level"` // instrument.TrigLevelSaved's word
	TrigSrc   int       `json:"trig_src"`
	Rising    bool      `json:"rising"`
	TrigType  int       `json:"trig_type"`
}

// ParseRefs decodes stored references; any structural problem or another
// version is an error (the scope starts with no references).
// TRLC-LINKS: REQ-DSO-138
func ParseRefs(raw []byte) (Refs, error) {
	if len(raw) > MaxFileSize {
		return Refs{}, fmt.Errorf("references: %d bytes, more than %d", len(raw), MaxFileSize)
	}
	var r Refs
	if err := json.Unmarshal(raw, &r); err != nil {
		return Refs{}, fmt.Errorf("references: %w", err)
	}
	if r.Version != RefsVersion {
		return Refs{}, fmt.Errorf("references: version %d, this firmware reads %d", r.Version, RefsVersion)
	}
	for i := range r.Slots {
		if s := &r.Slots[i]; len(s.Cols) > MaxChannels {
			return Refs{}, fmt.Errorf("references: slot %d holds %d channels", i, len(s.Cols))
		}
	}
	return r, nil
}

// LoadRefs reads the stored references; ok false for none (missing,
// unreadable, corrupt).
// TRLC-LINKS: REQ-DSO-138
func LoadRefs(kv hal.KV, logf func(string, ...any)) (Refs, bool) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if kv == nil {
		return Refs{}, false
	}
	raw, ok := read(kv, RefsKey, logf)
	if !ok {
		return Refs{}, false
	}
	r, err := ParseRefs(raw)
	if err != nil {
		logf("settings: %s: %v — no references", nameOf(kv, RefsKey), err)
		return Refs{}, false
	}
	return r, true
}

// SaveRefs stores r, leaving a store that already holds its bytes as it is.
// TRLC-LINKS: REQ-DSO-138
func SaveRefs(kv hal.KV, r Refs) error {
	if kv == nil {
		return errors.New("settings: no store")
	}
	r.Version = RefsVersion
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if old, ok := kv.Get(RefsKey); ok && bytes.Equal(old, b) {
		return nil
	}
	return kv.Put(RefsKey, b)
}

// RefsSource is what holds the references: their state and a count that
// moves whenever one is stored, shown, hidden or cleared.
type RefsSource interface {
	RefsState() Refs
	RefsGen() uint64
}

// WatchRefs stores the references whenever they change (checked every
// poll), until stop; a write that fails is logged and retried at the next
// change. Nothing is written for references never touched since start.
// TRLC-LINKS: REQ-DSO-138
func WatchRefs(kv hal.KV, src RefsSource, poll time.Duration, stop <-chan struct{}, logf func(string, ...any)) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	last := src.RefsGen()
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		}
		if g := src.RefsGen(); g != last {
			if err := SaveRefs(kv, src.RefsState()); err != nil {
				logf("settings: save %s: %v", nameOf(kv, RefsKey), err)
				continue
			}
			last = g
		}
	}
}
