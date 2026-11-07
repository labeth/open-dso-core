// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import (
	"time"
)

// RecordNeed is what a reader of live frames needs of their records beyond
// what the scope picks for the screen: a bus search, an FFT, an eye diagram
// or a bus decode reads samples, not the screen's points. The owner asks the
// board for the deepest of the needs leased now; none is a setting, so none
// is saved or reported as the instrument's state.
type RecordNeed struct {
	// Samples is the least samples across the screen (0: the scope's).
	Samples int
	// SampleS is the coarsest sample interval (0: none asked); the owner
	// turns it into samples across the screen at the timebase set.
	SampleS float64
	// FullRate asks for every converter sample, as deep as a record goes.
	FullRate bool
}

// merge is the need that serves both n and o.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010
func (n RecordNeed) merge(o RecordNeed) RecordNeed {
	n.Samples = max(n.Samples, o.Samples)
	if o.SampleS > 0 && (n.SampleS <= 0 || o.SampleS < n.SampleS) {
		n.SampleS = o.SampleS
	}
	n.FullRate = n.FullRate || o.FullRate
	return n
}

// lease is one holder's need and when it ends.
type lease struct {
	need RecordNeed
	end  time.Time
}

// maxLease bounds a lease: a holder renews it while it reads, so a holder
// gone (a closed page) cannot keep deep records on.
const maxLease = 10 * time.Second

// Lease asks for records serving n for d from now as holder, replacing
// holder's earlier lease; d <= 0 ends it. A holder renews it while it
// reads.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010
func (m *Model) Lease(holder string, n RecordNeed, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d <= 0 {
		delete(m.leases, holder)
		return
	}
	if m.leases == nil {
		m.leases = map[string]lease{}
	}
	m.leases[holder] = lease{need: n, end: time.Now().Add(min(d, maxLease))}
}

// RecordNeed is the need of every lease current now (OWNER): the owner
// commits again when it changes.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010
func (m *Model) RecordNeed() RecordNeed {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var n RecordNeed
	for h, l := range m.leases {
		if !now.Before(l.end) {
			delete(m.leases, h)
			continue
		}
		n = n.merge(l.need)
	}
	return n
}
