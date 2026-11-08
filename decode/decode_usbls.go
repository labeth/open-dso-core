// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package decode

import (
	"fmt"
	"math"
	"strings"
)

// USBLSCfg configures the USB low/full-speed decode of the single D+ line.
//
//	Bitrate   0 => infer the bit period from the shortest level-run (= one bit);
//	          else bits/s (LS = 1_500_000, FS = 12_000_000).
//	Threshold /HaveThr override the auto slice threshold (see sliceChannel).
//
// TRLC-LINKS: REQ-DSO-018
type USBLSCfg struct {
	Bitrate   int
	Threshold float64
	HaveThr   bool
}

// usbPIDName maps a 4-bit PID value (PID3..PID0, the low nibble transmitted
// LSB-first) to its packet name. The full 8-bit PID byte on the wire is this
// nibble followed by its ones-complement, so e.g. DATA0 (nibble 0x3) => 0xC3.
var usbPIDName = map[int]string{
	0x1: "OUT", 0x9: "IN", 0x5: "SOF", 0xD: "SETUP",
	0x3: "DATA0", 0xB: "DATA1", 0x7: "DATA2", 0xF: "MDATA",
	0x2: "ACK", 0xA: "NAK", 0xE: "STALL", 0x6: "NYET",
	0xC: "PRE", 0x8: "SPLIT", 0x4: "PING",
}

// USBLS decodes USB low/full-speed packets on one channel's codes — the
// D+ single-ended line, where the J/K bus states appear as the two logic
// levels. Mirrors decode_usbls.js step for step so LCD and web agree byte-for-
// byte.
//
// Line coding is NRZI: a 0 bit is a level TRANSITION, a 1 bit is NO transition.
// Bit stuffing inserts a 0 after six consecutive 1s (dropped on decode). A
// packet is SYNC (00000001 = KJKJKJKK) + PID (4 bits + 4-bit complement) +
// optional data/CRC + EOP (~2 bit-times of SE0, which on the single D+ line
// reads as an extended idle that, with the inter-packet idle, bounds the packet).
// TRLC-LINKS: REQ-DSO-018
func USBLS(dp []uint8, colTimeS float64, cfg USBLSCfg) Result {
	const minSPB = 4.0  // samples per bit floor
	const splitK = 10.0 // inter-packet idle: a gap wider than this many bit periods
	S := sliceChannel(dp, cfg.Threshold, cfg.HaveThr)
	if !S.ok {
		return Result{Proto: protoUSBLS, Error: S.reason}
	}
	if len(S.edges) < 8 { // SYNC alone carries 7 transitions
		return Result{Proto: protoUSBLS, Error: msgTooFewEdges}
	}

	// Bit period T (samples/bit). cfg.Bitrate pins it; otherwise infer it: NRZI
	// edge gaps are integer multiples of one bit period (1..7, capped by bit
	// stuffing), so the shortest cluster is T. A blind low percentile broke on
	// realistic idle-bus captures — a keep-alive train (one 2-bit SE0 every
	// ~1 ms) floods the gap list with 2-bit and huge inter-KA gaps, drags the
	// percentile past the packet's few 1-bit gaps, and the collapsed estimate
	// made auto decode fail on a capture sigrok reads fine (found by the
	// sigrok oracle). Use inferUARTspb's deterministic cluster walk instead:
	// each ascending gap cluster is tried as the 1-bit hypothesis, refined by
	// re-centered mean, validated by the fraction of gaps it explains as
	// integer bit multiples; ties go to the larger period. Gaps beyond ~16
	// candidate bits (inter-packet / keep-alive spacing) carry no bit-timing
	// evidence and are excluded from refine and validation.
	var T float64
	if cfg.Bitrate > 0 {
		T = (1.0 / float64(cfg.Bitrate)) / colTimeS
	} else {
		var why string
		if T, why = usblsBitPeriod(S.edges); why != "" {
			return Result{Proto: protoUSBLS, Error: why}
		}
	}
	if math.IsInf(T, 0) || math.IsNaN(T) || !(T >= minSPB) {
		return Result{Proto: protoUSBLS, Error: fmt.Sprintf("%.1f samples/bit; need >= %g", T, minSPB)}
	}

	// Segment edges into PACKETS on the inter-packet idle. Within a packet bit
	// stuffing guarantees a transition at least every ~7 bit periods; the EOP
	// then the idle line open a much wider gap, so split where consecutive edges
	// are more than splitK bit periods apart. Packets closer than that (USB
	// allows two bit times between them) are split at their EOPs (usbSplits).
	var segs [][2]int
	segStart := 0
	for k := 1; k < len(S.edges); k++ {
		if float64(S.edges[k].i-S.edges[k-1].i) > splitK*T {
			segs = append(segs, [2]int{segStart, k - 1})
			segStart = k
		}
	}
	segs = append(segs, [2]int{segStart, len(S.edges) - 1})

	var spans []Span
	var bytesOut []int
	var toks []string
	packets := 0
	emit := func(p usbPacket) {
		if packets > 0 { // separate packets in the transcript
			spans = append(spans, Span{p.spans[0].I0, p.spans[0].I0, "", "gap", 0})
			toks = append(toks, "|")
		}
		packets++
		spans = append(spans, p.spans...)
		toks = append(toks, p.toks...)
		bytesOut = append(bytesOut, p.bytes...)
	}
	for sgIdx, sg := range segs {
		last := sgIdx == len(segs)-1
		for _, sub := range usbSplits(&S, T, sg) {
			if p, ok := usbSegment(&S, T, sub, last && sub[1] == sg[1]); ok {
				emit(p)
			}
		}
	}
	if packets == 0 {
		return Result{Proto: protoUSBLS, Error: "no USB packet (SYNC+PID) found"}
	}

	baud := cfg.Bitrate
	if colTimeS > 0 {
		baud = snapRate(int(math.Round(1.0/(T*colTimeS))), 1.5e6, 12e6)
	}
	return Result{
		OK: true, Proto: protoUSBLS, Spans: spans, Text: strings.Join(toks, " "),
		Bytes: bytesOut, Baud: baud, SPB: T, Thr: S.threshold,
	}
}

// usbSplits splits a segment of packets closer than splitK bit times apart
// at their EOPs: on the line idling at J an EOP is a K-level run of two bit
// times or more (the SE0) and a J run (the packet's closing J and the
// inter-packet idle, two bit times or more), then the next packet's SYNC
// (KJKJKJK, one bit time each, and its closing K). A stretch of data may
// look so; the split stands only where the packet before it checks whole
// (usbSegment's valid: PID, CRC, stuffing).
// TRLC-LINKS: REQ-DSO-018
func usbSplits(sl *sliced, T float64, sg [2]int) [][2]int { //nolint:gocritic // T is the bit period in samples, the name the decoder's comments use
	e := sl.edges
	jDir := e[sg[1]].dir // the edge into the idle after the segment
	var out [][2]int
	start := sg[0]
	for k := start + 1; k+8 <= sg[1]; k++ {
		if e[k].dir != jDir || e[k].x-e[k-1].x < 1.5*T || e[k+1].x-e[k].x < 1.5*T || e[k+8].x-e[k+7].x < 1.5*T {
			continue
		}
		sync := true
		for j := 1; j <= 6 && sync; j++ {
			sync = math.Abs(e[k+1+j].x-e[k+j].x-T) <= 0.4*T
		}
		if !sync {
			continue
		}
		if p, ok := usbSegment(sl, T, [2]int{start, k}, false); ok && p.valid {
			out = append(out, [2]int{start, k})
			start = k + 1
			k += 7
		}
	}
	return append(out, [2]int{start, sg[1]})
}

// usbPacket is one packet USBLS read: its spans, transcript tokens and
// bytes, and whether every check held (the PID's complement, a token's
// CRC5 or a data packet's CRC16, the bit stuffing).
type usbPacket struct {
	spans []Span
	toks  []string
	bytes []int
	valid bool
}

// usbSegment decodes the packet whose edges are sg (last: the record's last
// segment, which needs idle after it), or reports none there.
// TRLC-LINKS: REQ-DSO-018
func usbSegment(sl *sliced, T float64, sg [2]int, last bool) (usbPacket, bool) { //nolint:gocritic,gocyclo // T the bit period in samples; a protocol state machine kept step for step with its decode_*.js twin (parity tests)
	const eopCells = 2 // EOP ~ 2 bit-times of SE0 trailing every packet
	n := sl.n
	clampI := func(i int) int {
		if i < 0 {
			return 0
		}
		if i >= n {
			return n - 1
		}
		return i
	}
	cellStart := func(x0 float64, k int) int { return clampI(int(math.Round(x0 + float64(k)*T))) }
	cellEnd := func(x0 float64, k int) int { return clampI(int(math.Round(x0+float64(k+1)*T)) - 1) }
	var spans []Span
	var toks []string
	var bytesOut []int
	if sg[1] <= sg[0] { // a lone edge carries no packet
		return usbPacket{}, false
	}
	x0, x1 := sl.edges[sg[0]].x, sl.edges[sg[1]].x
	nCells := int(math.Round((x1 - x0) / T))
	if nCells < 8+8+eopCells { // SYNC + PID + EOP minimum
		return usbPacket{}, false
	}
	if nCells > n { // safety cap
		nCells = n
	}
	// A record that ends mid-transmission leaves the trailing packet with no
	// EOP/idle to close it — require trailing idle on the last segment and drop
	// the partial otherwise (real captures start/end at a random phase).
	if last && float64(n-1-sl.edges[sg[1]].i) < 2*T {
		return usbPacket{}, false
	}

	// NRZI-decode the cells: idle level seeds the first comparison, then a level
	// change vs the previous cell = 0, no change = 1.
	idleX := x0 - 0.5*T
	if idleX < 0 {
		idleX = 0
	}
	if idleX > float64(n-1) {
		idleX = float64(n - 1)
	}
	prev := max(logicAt(sl, idleX), 0)
	var rawBits, rawCell []int
	// Re-synchronize the cell grid on edges (as a USB receiver's DPLL does):
	// an edge within 0.4 bit of a cell boundary re-anchors the grid there.
	// Free-running from the first edge, a 1 % bit-time error slips bits
	// across a packet (0F F0 00 F8 reads A7 F4 01 FC at 5.5 samples a
	// bit).
	// The grid, not a count fixed up front, says where the packet ends: its
	// last cell starts before the last edge (rounding (x1-x0)/T dropped the
	// final byte when T read a little long).
	ax, ak, ei := x0, 0, sg[0]
	for k := range nCells + eopCells {
		xs := ax + float64(k-ak)*T
		if xs > x1-0.5*T {
			break
		}
		for ei <= sg[1] && sl.edges[ei].x < xs-0.4*T {
			ei++
		}
		if ei <= sg[1] && math.Abs(sl.edges[ei].x-xs) <= 0.4*T {
			ax, ak = sl.edges[ei].x, k
		}
		lv := logicAt(sl, ax+(float64(k-ak)+0.5)*T)
		if lv < 0 { // ran off the captured region
			break
		}
		b := 1
		if lv != prev {
			b = 0
		}
		prev = lv
		rawBits = append(rawBits, b)
		rawCell = append(rawCell, k)
	}
	// Strip the trailing EOP cells (SE0 reads as an extended idle bound here).
	if len(rawBits) < eopCells+16 {
		return usbPacket{}, false
	}
	rawBits = rawBits[:len(rawBits)-eopCells]
	rawCell = rawCell[:len(rawCell)-eopCells]

	// De-stuff: the 0 inserted after six consecutive 1s is dropped. Track each
	// kept bit's raw cell so spans map back to sample indices.
	var bitsArr, cellOf []int
	ones := 0
	stuffCell := -1
	for i := range rawBits {
		if ones == 6 {
			// EOP cells are already removed. An observed seventh one is a
			// malformed packet, not a clean prefix eligible for triggering.
			if rawBits[i] != 0 {
				stuffCell = rawCell[i]
				break
			}
			ones = 0
			continue // drop the stuffed 0
		}
		bitsArr = append(bitsArr, rawBits[i])
		cellOf = append(cellOf, rawCell[i])
		if rawBits[i] == 1 {
			ones++
		} else {
			ones = 0
		}
	}
	if len(bitsArr) < 16 {
		return usbPacket{}, false
	}
	// SYNC must be 00000001; anything else is a partial/garbage frame — drop it.
	syncOK := bitsArr[7] == 1
	for i := range 7 {
		if bitsArr[i] != 0 {
			syncOK = false
		}
	}
	if !syncOK {
		return usbPacket{}, false
	}
	// PID = 8 bits LSB-first: low nibble = PID, high nibble = its complement.
	pidByte := 0
	for i := range 8 {
		pidByte |= bitsArr[8+i] << i
	}
	pid4 := pidByte & 0xF
	check := (pidByte >> 4) & 0xF
	name := usbPIDName[pid4]
	if name == "" {
		name = fmt.Sprintf("PID%X", pid4)
	}
	pidBad := check != (^pid4 & 0xF)

	spans = append(spans, Span{cellStart(x0, cellOf[0]), cellEnd(x0, cellOf[7]), "SYNC", kindStart, 0})
	pidText, pidKind := name, kindAddr
	if pidBad {
		pidText, pidKind = "!"+name, kindFrameError
	}
	spans = append(spans, Span{cellStart(x0, cellOf[8]), cellEnd(x0, cellOf[15]), pidText, pidKind, pid4})
	toks = append(toks, pidText)

	// Payload bytes (data + CRC) after the PID, LSB-first. A DATA packet's
	// last two bytes are its CRC16: a mismatch marks them, so bytes misread
	// from a degraded capture are never shown as good.
	nBytes := (len(bitsArr) - 16) / 8
	payload := make([]int, nBytes)
	for b := range nBytes {
		base := 16 + b*8
		for i := range 8 {
			payload[b] |= bitsArr[base+i] << i
		}
	}
	crcBad := usbIsData(pid4) && nBytes >= 2 && usbCRC16(payload[:nBytes-2]) != payload[nBytes-2]|payload[nBytes-1]<<8
	if usbIsToken(pid4) && nBytes == 2 { // address and endpoint (SOF: frame number) and their CRC5
		w := payload[0] | payload[1]<<8
		crcBad = usbCRC5(w&0x7ff) != w>>11
	}
	for b, val := range payload {
		base := 16 + b*8
		text, kind := hex2(val), kindData
		if crcBad && b >= nBytes-2 {
			text, kind = "!"+text, "crc-error"
		}
		spans = append(spans, Span{cellStart(x0, cellOf[base]), cellEnd(x0, cellOf[base+7]), text, kind, val})
		toks = append(toks, text)
		bytesOut = append(bytesOut, val)
	}
	if stuffCell >= 0 {
		spans = append(spans, Span{cellStart(x0, stuffCell), cellEnd(x0, stuffCell), "STUFF!", kindFrameError, 0})
		toks = append(toks, "STUFF!")
	}
	return usbPacket{spans, toks, bytesOut, !pidBad && !crcBad && stuffCell < 0}, true
}

// usblsBitPeriod infers a USB low/full-speed line's bit period, in samples,
// from its edges (USBLS tells how), or why it cannot.
// TRLC-LINKS: REQ-DSO-018
func usblsBitPeriod(edges []edge) (float64, string) {
	var gaps []float64
	for k := 1; k < len(edges); k++ {
		if g := float64(edges[k].i - edges[k-1].i); g >= 1 {
			gaps = append(gaps, g)
		}
	}
	if len(gaps) < 3 {
		return 0, "too few edges / cannot infer bitrate"
	}
	cands := gapClusters(gaps)
	best, bestFrac := 0.0, -1.0
	for _, cand := range cands {
		if cand < 2.5 { // below the samples/bit floor: a spur cluster
			continue
		}
		kg := make([]float64, 0, len(gaps))
		for _, g := range gaps {
			if g <= 16*cand {
				kg = append(kg, g)
			}
		}
		if len(kg) < 3 {
			continue
		}
		ref := cand
		for range 2 {
			sum, cnt := 0.0, 0
			for _, g := range kg {
				if math.Abs(g-ref) <= 0.35*ref {
					sum += g
					cnt++
				}
			}
			if cnt > 0 {
				ref = sum / float64(cnt)
			}
		}
		good := 0
		for _, g := range kg {
			if m := math.Round(g / ref); m >= 1 && math.Abs(g-m*ref) <= 0.35*ref {
				good++
			}
		}
		// >= keeps the LARGER candidate on an exact tie (candidates ascend).
		if frac := float64(good) / float64(len(kg)); frac >= 0.7 && frac >= bestFrac {
			best, bestFrac = ref, frac
		}
	}
	if best <= 0 {
		return 0, "bitrate ambiguous — set it explicitly"
	}
	return best, ""
}

// usbIsToken reports a token PID (OUT, IN, SOF, SETUP, PING), whose 11
// bits after the PID end in a CRC5.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-237
func usbIsToken(pid4 int) bool { return pid4&3 == 1 || pid4 == 0x4 }

// usbCRC5 is the USB token CRC: x^5+x^2+1 over the 11 bits in wire order
// (least significant first) from all ones, inverted, its highest term sent
// first: as the field reads least significant bit first, bit-reversed.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-237
func usbCRC5(v int) int {
	c := 0x1F
	for i := range 11 {
		c <<= 1
		if v>>i&1 != c>>5 {
			c ^= 0x25
		}
		c &= 0x1F
	}
	c ^= 0x1F
	return c>>4&1 | c>>2&2 | c&4 | c<<2&8 | c<<4&16
}

// usbIsData reports a DATA0/DATA1/DATA2/MDATA PID, whose payload ends in a CRC16.
// TRLC-LINKS: REQ-DSO-018
func usbIsData(pid4 int) bool { return pid4&3 == 3 }

// usbCRC16 is the USB data CRC: polynomial 0x8005 reflected (0xA001), initial
// all ones, inverted; sent low byte first.
// TRLC-LINKS: REQ-DSO-018
func usbCRC16(data []int) int {
	c := 0xFFFF
	for _, b := range data {
		c ^= b & 0xFF
		for range 8 {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
	}
	return c ^ 0xFFFF
}
