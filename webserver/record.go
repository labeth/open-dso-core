// ENGMODEL-OWNER-UNIT: FU-DSO-WEBSERVER

package webserver

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/labeth/open-dso-core/webserver/wire"
)

// recordWaitMax bounds /api/record.bin's wait for a record.
const recordWaitMax = 10 * time.Second

// RecordNever is the refusal no retry changes: this acquisition serves no
// sampled records (the page's Live audio stops on it).
const RecordNever = "this acquisition cannot serve sampled records"

// hRecord serves one whole sampled record of both channels, taken every
// ?sample_s or finer, captured after the request (?wait_ms, at most 10 s,
// 4 s by default): the BUS Visual view decodes digital audio from it (the
// live frames carry a screen's few thousand values, the record up to the
// memory's half million a channel). The reply is a little-endian uint32
// header length, the JSON header (wire.RecordReply) and the codes, C1's
// then C2's (or, as the header's chans names them, each channel's in turn). One record is taken at a time. A record refused (another under
// way, none in time, none this acquisition can serve) is a 409 naming why.
// TRLC-LINKS: REQ-DSO-280
func (s *Server) hRecord(w http.ResponseWriter, r *http.Request) {
	want, err := strconv.ParseFloat(r.URL.Query().Get("sample_s"), 64)
	if err != nil || !(want >= 2e-9) || want > 1 {
		http.Error(w, "sample_s: the sample interval wanted, in seconds (2e-9 or more)", http.StatusBadRequest)
		return
	}
	if !s.recordBusy.CompareAndSwap(false, true) {
		w.Header().Set("Retry-After", "1")
		recordRefused(w, "another record is being taken: one at a time")
		return
	}
	defer s.recordBusy.Store(false)
	wait := 4 * time.Second
	if ms, err := strconv.Atoi(r.URL.Query().Get("wait_ms")); err == nil && ms > 0 {
		wait = min(time.Duration(ms)*time.Millisecond, recordWaitMax)
	}
	rs, ok := s.sc.Records()
	if !ok {
		recordRefused(w, RecordNever)
		return
	}
	rec, err := rs.Take(r.Context(), want, wait)
	if err != nil {
		// the scope's state, not the server, keeps the record from coming: a conflict, said why
		recordRefused(w, err.Error())
		return
	}
	var out []byte
	if len(rec.Chans) > 0 {
		out = encodeChans(rec)
	} else {
		n := min(len(rec.C1), len(rec.C2))
		hdr := wire.RecordReply{
			Seq: rec.Seq, N: n, SampleS: rec.SampleS, TdivS: rec.TdivS,
			Vpc1: rec.Vpc[0], Vpc2: rec.Vpc[1], Off1V: rec.Off[0], Off2V: rec.Off[1],
		}
		out = wire.EncodeRecord(hdr, rec.C1[:n], rec.C2[:n])
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// recordRefused answers a record refused for the scope's state: 409 and why.
// TRLC-LINKS: REQ-DSO-280
func recordRefused(w http.ResponseWriter, why string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(wire.RecordReply{Err: why})
}

// encodeChans lays out a record of any channels: the header names them
// (chans) with their scales, vpc1/off1_v and vpc2/off2_v those of C1 and C2
// where the record has them.
// TRLC-LINKS: REQ-DSO-280
func encodeChans(rec wire.Record) []byte {
	n := len(rec.Chans[0].Codes)
	for _, c := range rec.Chans {
		n = min(n, len(c.Codes))
	}
	hdr := wire.RecordReply{Seq: rec.Seq, N: n, SampleS: rec.SampleS, TdivS: rec.TdivS}
	codes := make([][]byte, 0, len(rec.Chans))
	for _, c := range rec.Chans {
		hdr.Chans = append(hdr.Chans, c.Ch)
		hdr.Vpc = append(hdr.Vpc, c.Vpc)
		hdr.Off = append(hdr.Off, c.Off)
		switch c.Ch {
		case 0:
			hdr.Vpc1, hdr.Off1V = c.Vpc, c.Off
		case 1:
			hdr.Vpc2, hdr.Off2V = c.Vpc, c.Off
		}
		codes = append(codes, c.Codes[:n])
	}
	return wire.EncodeRecordN(hdr, codes...)
}
