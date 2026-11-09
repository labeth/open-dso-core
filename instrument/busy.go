// ENGMODEL-OWNER-UNIT: FU-DSO-INSTRUMENT

package instrument

import "time"

// busy is what the owner is busy with for long enough to say on screen
// (an FPGA image load), and the image it runs on.
type busy struct {
	what  string
	since time.Time
	image string
	band  string
}

// SetBusy says what the owner is busy with ("" when it is done), for the
// loading indicator (Stats.Busy, BusyMs from the first SetBusy of a
// stretch; its progress is not known). OWNER.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-168
func (m *Model) SetBusy(what string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if what != "" && m.busy.what == "" {
		m.busy.since = time.Now()
	}
	m.busy.what = what
}

// SetImage names the FPGA image the board runs on (Stats.Image). OWNER.
// TRLC-LINKS: REQ-DSO-011
func (m *Model) SetImage(name string) {
	m.mu.Lock()
	m.busy.image = name
	m.mu.Unlock()
}

// SetBand is the band word every capture's status reports
// (Stats.BandKind; hal.Profile.StatusBand). OWNER.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-010
func (m *Model) SetBand(word string) {
	m.mu.Lock()
	m.busy.band = word
	m.mu.Unlock()
}

// fill puts what the owner is busy with and the image into st.
// TRLC-LINKS: REQ-DSO-008, REQ-DSO-168
func (b busy) fill(st *Stats) {
	st.Image, st.BandKind = b.image, b.band
	if b.what != "" {
		st.Busy, st.BusyMs, st.BusyPct = b.what, float64(time.Since(b.since))/float64(time.Millisecond), -1
	}
}
