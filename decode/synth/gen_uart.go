// ENGMODEL-OWNER-UNIT: FU-DSO-DECODE

package synth

// Waveform generators for the protocols beyond the first ten, shared by
// their round-trip, break, parity, oracle and detection tests. Each writes
// logic levels on a timeline (oracle_test.go) at a sample rate, so
// non-integer samples per bit accumulate like a real capture.

// UARTChar appends one UART character (start, 8 data bits LSB first, parity
// "even"/"odd"/"none", stops stop bits) at baud.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func UARTChar(w *Line, v int, baud float64, parity string, stops int) {
	bit := 1 / baud
	w.Add(0, bit)
	ones := 0
	for i := range 8 {
		b := byte(v >> i & 1)
		ones += int(b)
		w.Add(b, bit)
	}
	switch parity {
	case parityEven:
		w.Add(byte(ones&1), bit)
	case "odd":
		w.Add(byte(1-ones&1), bit)
	}
	w.Add(1, float64(stops)*bit)
}

// LINFrame appends a LIN frame: a break of breakBits, a one-bit delimiter,
// the sync, the protected identifier of id and, unless data is nil, the
// response with its checksum (enhanced unless classic or a diagnostic ID).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func LINFrame(w *Line, baud, breakBits float64, id int, data []int, classic bool) {
	bit := 1 / baud
	w.Add(0, breakBits*bit)
	w.Add(1, bit)
	UARTChar(w, 0x55, baud, "none", 1)
	pid := LINPID(id)
	UARTChar(w, pid, baud, "none", 1)
	if data == nil {
		w.Add(1, 30*bit)
		return
	}
	w.Add(1, 2*bit) // response space
	for _, d := range data {
		UARTChar(w, d, baud, "none", 1)
	}
	enh := !classic && id != 0x3c && id != 0x3d
	UARTChar(w, LINChecksum(pid, data, enh), baud, "none", 1)
	w.Add(1, 30*bit)
}

// DMXPacket appends a DMX512 packet: a break of breakUs, a mark after break
// of mabUs, the start code and the slots (8N2 at 250 kbit/s), then 40 us of
// mark (under a break's 88 us: sigrok's decoder takes a longer mark for a
// break of the other polarity).
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func DMXPacket(w *Line, breakUs, mabUs float64, start int, slots []int) {
	DMXPacketGap(w, breakUs, mabUs, 0, start, slots)
}

// DMXPacketGap is DMXPacket with mbsUs of mark between the slots.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func DMXPacketGap(w *Line, breakUs, mabUs, mbsUs float64, start int, slots []int) {
	w.Add(0, breakUs*1e-6)
	w.Add(1, mabUs*1e-6)
	UARTChar(w, start, 250000, "none", 2)
	for _, s := range slots {
		w.Add(1, mbsUs*1e-6)
		UARTChar(w, s, 250000, "none", 2)
	}
	w.Add(1, 40e-6)
}

// ModbusFrame appends an RTU frame with its CRC, then 4 characters of idle.
// TRLC-LINKS: REQ-DSO-018, REQ-DSO-232
func ModbusFrame(w *Line, baud float64, parity string, stops int, b []int) {
	crc := ModbusCRC(b)
	for _, v := range append(append([]int{}, b...), crc&0xff, crc>>8) {
		UARTChar(w, v, baud, parity, stops)
	}
	w.Add(1, 44/baud)
}
