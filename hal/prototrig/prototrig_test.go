package prototrig

import (
	"testing"

	"github.com/labeth/open-dso-core/hal"
)

// TRLC-LINKS: REQ-DSO-1001
func TestConfigurationsAreProtocolTriggers(t *testing.T) {
	for want, p := range map[string]hal.ProtoTrigger{
		"i2c": I2C{}, "spi": SPI{}, "uart": UART{}, "can": CAN{}, "lin": LIN{},
	} {
		if p.Protocol() != want {
			t.Errorf("%T names %q, want %q", p, p.Protocol(), want)
		}
	}
}
