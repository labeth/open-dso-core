package engine

import (
	"testing"
)

// A request that leaves a rate to the decoder takes the rate the records
// measured, and waits on it until then; its bytes become the matcher's
// units, and an ARINC 429 match of two words is refused with the reason.
// TRLC-LINKS: REQ-DSO-013
func TestHALSerialPlansMeasuredRates(t *testing.T) {
	c := &HALCycle{}
	if _, why := c.planRates(SerialParams{Proto: SerUART}); why != "measuring the UART bit rate" {
		t.Fatalf("why %q", why)
	}
	c.ser.baud = 9600.7
	if p, why := c.planRates(SerialParams{Proto: SerUART}); why != "" || p.Baud != 9600 {
		t.Fatalf("%+v %q", p, why)
	}
	if _, why := c.planRates(SerialParams{Proto: SerSPI}); why != SPIMeasuring {
		t.Fatalf("why %q", why)
	}
	c.ser.spiHz = 1.0000004e6
	if p, why := c.planRates(SerialParams{Proto: SerSPI}); why != "" || p.SPIClockHz != 1000000 {
		t.Fatalf("%+v %q", p, why)
	}
	c.ser.spiProbe = true
	if _, why := c.planRates(SerialParams{Proto: SerSPI}); why != SPIMeasuring {
		t.Fatalf("a clock measured again: why %q", why)
	}
	if p, _ := c.planRates(SerialParams{Proto: SerUART, Baud: 115200}); p.Baud != 115200 {
		t.Fatalf("a rate of the request's own replaced: %+v", p)
	}
	if _, why := c.planRates(SerialParams{Proto: SerARINC, Baud: 100000, Bytes: []int{0, 1, 2, 0, 3, 4}}); why != ArincOneWord {
		t.Fatalf("why %q", why)
	}
}
