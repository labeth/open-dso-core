// ENGMODEL-OWNER-UNIT: FU-DSO-STREAMVIEW

package streamview

// ID names one decoded stream's capture: its event epoch and record.
type ID struct {
	Epoch  uint32 `json:"epoch"`
	Record uint32 `json:"record"`
}

// Status is what the controller reads of a decoded stream's capture: the
// trigger it ended on and the record it keeps, with the record's span in
// the stream's samples when it has one.
type Status struct {
	Triggered, Frozen, Ready bool
	// HasSampleTimeline: SampleFirst and SampleLast are the record's first
	// and last stream samples, TriggerIndex its trigger in record words of
	// two samples.
	HasSampleTimeline       bool
	SampleFirst, SampleLast uint64
	TriggerIndex            uint32
	Length                  uint32 // the record's words
}
