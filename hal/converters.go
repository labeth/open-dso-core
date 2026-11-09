package hal

// ConverterMonitor is optional on a Platform: a board that checks its own
// converters (cores that stop converting after a configuration, say) and
// recovers them reports what it found, for the status and the screen's
// notice. OWNER checks; the report is ANY.
type ConverterMonitor interface {
	ConverterHealth() ConverterHealth // ANY
}

// ConverterHealth is a board's converter checks' findings.
type ConverterHealth struct {
	// Fault names the converters that do not convert, as the screen says it
	// ("" while every one converts).
	Fault string
	// Recoveries counts converters brought back, Recovery says how the last
	// came back.
	Recoveries int64
	Recovery   string
	// CheckMs is the last check's duration.
	CheckMs float64
}
