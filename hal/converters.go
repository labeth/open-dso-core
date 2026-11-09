package hal

import "context"

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

// ConverterChecker is optional on a Platform: a board whose converters
// must be looked at now and then while it acquires (a core that stopped
// converting after a configuration) says when a check is due, and the
// owner runs it between acquisitions while it runs captures, never inside
// a stream or a roll.
type ConverterChecker interface {
	CheckDue() bool // ANY: a periodic check is due
	// CheckConverters runs the check and brings back what stopped; a
	// recovery may load the image again, which loses the stored record and
	// whatever the fabric had applied (reloaded true: the owner commits
	// everything again). OWNER, LONG.
	CheckConverters(ctx context.Context) (reloaded bool)
}
