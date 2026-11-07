package hal

// BootTarget is the owner's choice of what the next power-up runs: this
// firmware or the vendor's. It is an optional capability of a board whose
// firmware starts from a vendor launch hook that can be told to leave the
// unit to the vendor firmware; the UI model's SYS sheet, SCPI and the web
// show and set it through one value. Next and Set touch no hardware (a file
// on the unit's storage, through the board's write guard). ANY.
type BootTarget interface {
	Next() string                 // BootOpen or BootVendor
	Set(target, why string) error // why is recorded with a vendor target
}

// The boot targets.
const (
	BootOpen   = "open"
	BootVendor = "vendor"
)

// BootTargetBoard is a board with the boot-target capability.
type BootTargetBoard interface {
	BootTarget() (BootTarget, bool) // ANY
}

// BootTargetOf is b's boot target; false when b has none.
// TRLC-LINKS: REQ-DSO-297
func BootTargetOf(b Board) (BootTarget, bool) {
	if bb, ok := b.(BootTargetBoard); ok {
		return bb.BootTarget()
	}
	return nil, false
}
