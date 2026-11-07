// ENGMODEL-OWNER-UNIT: FU-OTA-DIAG

package diag

import "golang.org/x/sys/unix"

// MonoMicros is CLOCK_MONOTONIC in microseconds: a board's trace and its
// step logs share it, so a step's traffic is found by its time.
// TRLC-LINKS: REQ-SDS-285
func MonoMicros() int64 {
	var ts unix.Timespec
	if unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts) != nil {
		return 0
	}
	return ts.Nano() / 1000
}
