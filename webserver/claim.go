// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Epoch is the single-active-client token. Each page load calls /api/claim,
// which bumps it; the frame.bin long-poll (the only sustained load) carries
// its claimed epoch and is refused (409) once a newer client has claimed. So
// a second browser "steals" the device and the first stops polling — the
// device serves at most ONE live client, bounding the CPU that competes with
// the acquisition. Epoch 0 (unclaimed: SCPI, curl, tests) is never refused.
// It starts at the server's start time, so a page claimed before a restart
// (its scripts the old build's) holds an epoch this run never issued and is
// refused once a page claims in this run.
type Epoch struct {
	cur   atomic.Uint64
	start uint64
}

// NewEpoch starts the epoch at the current time.
// TRLC-LINKS: REQ-DSO-161
func NewEpoch() *Epoch {
	e := &Epoch{start: uint64(time.Now().UnixMilli())} //nolint:gosec // G115: a wall clock after 1970
	e.cur.Store(e.start)
	return e
}

// Current is the epoch last issued (the start before any claim).
// TRLC-LINKS: REQ-DSO-161
func (e *Epoch) Current() uint64 { return e.cur.Load() }

// ServeClaim issues a fresh epoch. The page calls it once on load; opening a
// second browser bumps the epoch and takes over.
// TRLC-LINKS: REQ-DSO-161
func (e *Epoch) ServeClaim(w http.ResponseWriter, _ *http.Request) {
	n := e.cur.Add(1)
	w.Header().Set("Content-Type", "application/json")
	// The encoder's text for the map, written directly.
	// A write to a client that went away has no one to report to.
	_, _ = w.Write(strconv.AppendUint([]byte(`{"epoch":`), n, 10))
	_, _ = w.Write([]byte("}\n"))
}

// Superseded reports whether a request's ?epoch is not the active client's
// once a browser has claimed in this run: an older one (a newer browser has
// claimed) or one from before a restart. epoch 0 / absent (SCPI, curl,
// tests) is exempt, and so is every page until a claim in this run.
// TRLC-LINKS: REQ-DSO-161
func (e *Epoch) Superseded(r *http.Request) bool {
	q := r.URL.Query().Get("epoch")
	if q == "" {
		return false
	}
	var n uint64
	_, _ = fmt.Sscanf(q, "%d", &n) // unparsable: 0, exempt like an absent epoch
	cur := e.cur.Load()
	return n != 0 && cur != e.start && n != cur
}
