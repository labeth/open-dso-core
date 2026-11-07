package hal

import "errors"

// Refusal is a setting or feature the board cannot serve, with the real
// reason. It is never answered by substituting something else.
type Refusal struct {
	Feature string // what was asked for: "trigger zone", "2 mV/div at 10x"
	Reason  string // why it cannot be served, for the user
}

// Error returns "feature: reason".
// TRLC-LINKS: REQ-DSO-1001
func (r *Refusal) Error() string { return r.Feature + ": " + r.Reason }

// Refuse returns a *Refusal as an error.
// TRLC-LINKS: REQ-DSO-1001
func Refuse(feature, reason string) error { return &Refusal{Feature: feature, Reason: reason} }

// AsRefusal returns the *Refusal in err's chain, if there is one.
// TRLC-LINKS: REQ-DSO-1001
func AsRefusal(err error) (*Refusal, bool) {
	var r *Refusal
	ok := errors.As(err, &r)
	return r, ok
}

// ErrNotTaken is Platform.BringUp's answer when this firmware must not take
// the unit (its identity cannot be read, or the vendor's processes would not
// stop): the app exits and leaves the unit to the vendor firmware. It is not
// a fault to retry.
var ErrNotTaken = errors.New("hal: unit not taken")

// ErrTimeout is Acquirer.Wait's timeout: no record became ready in time.
// It is not a fault.
var ErrTimeout = errors.New("hal: wait timed out")
