// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"fmt"
)

// OwnerJob is work another goroutine hands the owner loop: run on the owner,
// unless the caller's context ended (cancelled reports it) before the owner
// came to it; the reply goes to done.
type OwnerJob struct {
	Cancelled func() error
	Run       func() (any, error)
	Done      chan OwnerReply
}

// OwnerReply is an owner job's result.
type OwnerReply struct {
	Value any
	Err   error
}

// Serve runs the job on the owner and reports whether it ran.
// TRLC-LINKS: REQ-SDS-001
func (j OwnerJob) Serve() bool {
	if err := j.Cancelled(); err != nil {
		j.Done <- OwnerReply{Err: err}
		return false
	}
	value, err := j.Run()
	j.Done <- OwnerReply{value, err}
	return true
}

// OwnerResult converts an owner call's reply to the type its run returns.
// TRLC-LINKS: REQ-SDS-001
func OwnerResult[T any](value any, err error) (T, error) {
	var zero T
	if err != nil {
		return zero, err
	}
	v, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("owner call returned %T, not %T", value, zero)
	}
	return v, nil
}
