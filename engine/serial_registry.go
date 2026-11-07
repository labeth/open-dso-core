// ENGMODEL-OWNER-UNIT: FU-APP-ENGINE

package engine

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/labeth/open-dso-core/decode"
)

// SerialDecoder receives chronological samples with their acquisition interval.
// It must not retain or mutate the sample slices. Registration is independent
// of acquisition, so additional decoders need no changes to the capture loop.
// TRLC-LINKS: REQ-SDS-013
type SerialDecoder struct {
	ID     int                                                               `json:"id"`
	Name   string                                                            `json:"name"`
	Decode func(a, b []uint8, sampleS float64, p SerialParams) decode.Result `json:"-"`
	Match  func([]decode.Span, SerialParams) (bool, int)                     `json:"-"`
	// Edges is Decode over digital lines' edges (decode.EdgeLine), for a
	// decoder that reads its lines only through their edges: the digital
	// stream decodes it in time that follows the bus's edges. Nil for the
	// decoders that read samples.
	Edges func(a, b *decode.EdgeLine, sampleS float64, p SerialParams) decode.Result `json:"-"`
}

var serialDecoders = struct {
	sync.RWMutex
	byID map[int]SerialDecoder
}{byID: map[int]SerialDecoder{}}

// RegisterSerialDecoder refuses replacement: an existing saved protocol ID
// must never silently acquire a different meaning.
// TRLC-LINKS: REQ-SDS-013
func RegisterSerialDecoder(d SerialDecoder) error {
	if d.ID <= 0 || d.Name == "" || d.Decode == nil || d.Match == nil {
		return errors.New("invalid serial decoder registration")
	}
	serialDecoders.Lock()
	defer serialDecoders.Unlock()
	if _, exists := serialDecoders.byID[d.ID]; exists {
		return fmt.Errorf("serial decoder %d already registered", d.ID)
	}
	serialDecoders.byID[d.ID] = d
	return nil
}

// SerialDecoderList is a sorted snapshot suitable for capability discovery.
// TRLC-LINKS: REQ-SDS-013
func SerialDecoderList() []SerialDecoder {
	serialDecoders.RLock()
	defer serialDecoders.RUnlock()
	result := make([]SerialDecoder, 0, len(serialDecoders.byID))
	for _, d := range serialDecoders.byID {
		result = append(result, d)
	}
	slices.SortFunc(result, func(a, b SerialDecoder) int { return cmp.Compare(a.ID, b.ID) })
	return result
}

// TRLC-LINKS: REQ-SDS-013
func LookupSerialDecoder(id int) (SerialDecoder, bool) {
	serialDecoders.RLock()
	d, ok := serialDecoders.byID[id]
	serialDecoders.RUnlock()
	return d, ok
}

// TRLC-LINKS: REQ-SDS-013
func init() {
	byteMatch := func(s []decode.Span, p SerialParams) (bool, int) { return matchBytes(s, p.Bytes) }
	for _, d := range serialDecoderTable() {
		if d.Match == nil {
			d.Match = byteMatch
		}
		switch {
		case d.ID == SerSENT, d.ID == SerCAN, d.ID == SerMIL1553, d.ID == SerARINC, d.ID == SerUSB, d.ID == SerFlexRay,
			d.ID >= SerLIN && d.ID != SerMIDI:
			d.Match = func(s []decode.Span, p SerialParams) (bool, int) {
				return matchPackets(s, p.Bytes)
			}
		}
		if err := RegisterSerialDecoder(d); err != nil {
			panic(err)
		}
	}
}

// serialDecoderTable is the built-in decoders; a nil Match is filled in
// by init.
// SPIDataDelayNs is where the scope's hardware SPI receiver reads the data
// line for a delay asked after the clock's sampling edge, with the clock on
// a channel: its register holds the delay in steps of its own, and the
// software decoder reads the line where the receiver does so both agree.
// Nil: at the delay asked.
var SPIDataDelayNs func(ns float64, clockCh int) float64

// TRLC-LINKS: REQ-SDS-013
func serialDecoderTable() []SerialDecoder {
	return []SerialDecoder{
		{ID: SerUART, Name: "UART", Decode: func(a, _ []uint8, sampleS float64, p SerialParams) decode.Result {
			return decode.UART(a, sampleS, decode.UARTCfg{Inverted: p.Inverted, Baud: p.Baud, Bits: p.Bits, Parity: p.Parity, Threshold: p.Threshold, HaveThr: p.HaveThr})
		}},
		{ID: SerI2C, Name: "I²C", Decode: func(a, b []uint8, sampleS float64, p SerialParams) decode.Result {
			return decode.I2C(a, b, sampleS, decode.I2CCfg{Inverted: p.Inverted, Threshold: p.Threshold, HaveThr: p.HaveThr})
		}, Match: matchI2C},
		{ID: SerSPI, Name: "SPI", Decode: func(a, b []uint8, sampleS float64, p SerialParams) decode.Result {
			cfg := decode.SPICfg{Inverted: p.Inverted, CPOL: p.CPOL, CPHA: p.CPHA, MSB: p.MSB, Threshold: p.Threshold, HaveThr: p.HaveThr, DataLag: decode.SPIDataLag(p.ChA, sampleS)}
			if sampleS > 0 {
				delayNs := p.SPIDelayNs
				if SPIDataDelayNs != nil {
					delayNs = SPIDataDelayNs(delayNs, p.ChA)
				}
				cfg.DataDelay = delayNs * 1e-9 / sampleS // as the scope's receiver reads it
			}
			return decode.SPI(a, b, sampleS, cfg)
		}},
		lineDecoder(SerManchester, "Manchester", func(p SerialParams) decode.ManchesterCfg {
			return decode.ManchesterCfg{Bitrate: p.Baud, IEEE: p.IEEE, MSB: p.MSB, Bits: p.Bits, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.Manchester, nil),
		lineDecoder(SerSENT, "SENT", func(p SerialParams) decode.SENTCfg {
			return decode.SENTCfg{Inverted: p.Inverted, TickNs: p.TickNs, Nibbles: p.Nibbles, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.SENT, nil),
		lineDecoder(SerCAN, "CAN FD", func(p SerialParams) decode.CANFDCfg {
			return decode.CANFDCfg{NominalBaud: p.Baud, DataBaud: p.DataBaud, DominantLow: true, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.CANFD, nil),
		lineDecoder(SerMIL1553, "MIL-STD-1553", func(p SerialParams) decode.MIL1553Cfg {
			return decode.MIL1553Cfg{Inverted: p.Inverted, Bitrate: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.MIL1553, nil),
		lineDecoder(SerARINC, "ARINC 429", func(p SerialParams) decode.ARINC429Cfg {
			return decode.ARINC429Cfg{Bitrate: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.ARINC429, nil),
		lineDecoder(SerUSB, "USB LS", func(p SerialParams) decode.USBLSCfg {
			return decode.USBLSCfg{Bitrate: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.USBLS, nil),
		lineDecoder(SerFlexRay, "FlexRay", func(p SerialParams) decode.FlexRayCfg {
			return decode.FlexRayCfg{Bitrate: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.FlexRay, nil),
		lineDecoder(SerLIN, "LIN", func(p SerialParams) decode.LINCfg {
			return decode.LINCfg{Baud: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.LIN, decode.LINEdges),
		lineDecoder(SerDMX, "DMX512", func(p SerialParams) decode.DMXCfg {
			return decode.DMXCfg{Baud: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.DMX512, decode.DMX512Edges),
		lineDecoder(SerMIDI, "MIDI", func(p SerialParams) decode.MIDICfg {
			return decode.MIDICfg{Baud: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.MIDI, nil),
		lineDecoder(SerModbus, "Modbus RTU", func(p SerialParams) decode.ModbusCfg {
			return decode.ModbusCfg{Baud: p.Baud, Parity: p.Parity, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.Modbus, decode.ModbusEdges),
		lineDecoder(SerOneWire, "1-Wire", func(p SerialParams) decode.OneWireCfg {
			return decode.OneWireCfg{Overdrive: p.Overdrive, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.OneWire, decode.OneWireEdges),
		lineDecoder(SerWS281x, "WS281x", func(p SerialParams) decode.WS281xCfg {
			return decode.WS281xCfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.WS281x, decode.WS281xEdges),
		lineDecoder(SerDHT, "DHT", func(p SerialParams) decode.DHTCfg {
			return decode.DHTCfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.DHT, decode.DHTEdges),
		lineDecoder(SerCEC, "HDMI-CEC", func(p SerialParams) decode.CECCfg {
			return decode.CECCfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.CEC, decode.CECEdges),
		lineDecoder(SerJ1850, "J1850 VPW", func(p SerialParams) decode.J1850Cfg {
			return decode.J1850Cfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.J1850, decode.J1850Edges),
		lineDecoder(SerIRNEC, "IR NEC", irCfg, decode.IRNEC, nil),
		lineDecoder(SerIRSIRC, "IR Sony", irCfg, decode.IRSIRC, nil),
		lineDecoder(SerIRRC5, "IR RC-5", irCfg, decode.IRRC5, nil),
		lineDecoder(SerIRRC6, "IR RC-6", irCfg, decode.IRRC6, nil),
		lineDecoder(SerDALI, "DALI", func(p SerialParams) decode.DALICfg {
			return decode.DALICfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.DALI, decode.DALIEdges),
		lineDecoder(SerUSBPD, "USB PD", func(p SerialParams) decode.USBPDCfg {
			return decode.USBPDCfg{Baud: p.Baud, Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.USBPD, decode.USBPDEdges),
		lineDecoder(SerSPDIF, "S/PDIF", func(p SerialParams) decode.SPDIFCfg {
			return decode.SPDIFCfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
		}, decode.SPDIF, decode.SPDIFEdges),
		clockedDecoder(SerPS2, "PS/2", decode.PS2, decode.PS2Edges),
		clockedDecoder(SerSWD, "SWD", decode.SWD, decode.SWDEdges),
		clockedDecoder(SerMDIO, "MDIO", decode.MDIO, decode.MDIOEdges),
		clockedDecoder(SerWiegand, "Wiegand", decode.Wiegand, decode.WiegandEdges),
		clockedDecoder(SerQuad, "Quadrature", decode.Quadrature, decode.QuadratureEdges),
		clockedDecoder(SerStepper, "Stepper", decode.Stepper, decode.StepperEdges),
		lineDecoder(SerPWM, "PWM", lineCfgFixed, decode.PWM, decode.PWMEdges),
		lineDecoder(SerRCEncode, "RC encode", lineCfgFixed, decode.RCEncode, decode.RCEncodeEdges),
		lineDecoder(SerOOK, "OOK", lineCfgFixed, decode.OOK, decode.OOKEdges),
		lineDecoder(SerMiller, "Miller", lineCfgRate, decode.Miller, decode.MillerEdges),
		lineDecoder(SerMorse, "Morse", lineCfgFixed, decode.Morse, decode.MorseEdges),
		lineDecoder(SerDCF77, "DCF77", lineCfgRate, decode.DCF77, decode.DCF77Edges),
		lineDecoder(SerEM4100, "EM4100", lineCfgRate, decode.EM4100, nil),
		lineDecoder(SerEM4305, "EM4305", lineCfgRate, decode.EM4305, nil),
		lineDecoder(SerT55xx, "T55xx", lineCfgRate, decode.T55xx, nil),
		lineDecoder(SerSWIM, "SWIM", lineCfgFixed, decode.SWIM, decode.SWIMEdges),
		lineDecoder(SerSDQ, "SDQ", lineCfgRate, decode.SDQ, decode.SDQEdges),
		lineDecoder(SerPJDL, "PJON", lineCfgFixed, decode.PJDL, decode.PJDLEdges),
		lineDecoder(SerDSI, "DSI", lineCfgFixed, decode.DSI, decode.DSIEdges),
		lineDecoder(SerQi, "Qi", lineCfgFixed, decode.Qi, decode.QiEdges),
		clockedDecoder(SerSDCard, "SD card", decode.SDCard, decode.SDCardEdges),
		clockedDecoder(SerPDI, "PDI", decode.PDI, decode.PDIEdges),
		clockedDecoder(SerCJTAG, "cJTAG", decode.CJTAG, decode.CJTAGEdges),
		clockedDecoder(SerMaple, "Maple", decode.Maple, decode.MapleEdges),
		clockedDecoder(SerCaliper, "Caliper", decode.Caliper, decode.CaliperEdges),
		lineDecoder(SerIRMP, "IR multi", lineCfgFixed, decode.IRMP, nil),
	}
}

// lineDecoder is a single-line decoder: samples reads the first line's
// samples, edges (nil for none) its edges, each with the settings cfg makes
// of the request.
// TRLC-LINKS: REQ-SDS-013
func lineDecoder[C any](id int, name string, cfg func(SerialParams) C,
	samples func([]uint8, float64, C) decode.Result, edges func(*decode.EdgeLine, float64, C) decode.Result,
) SerialDecoder {
	d := SerialDecoder{ID: id, Name: name, Decode: func(a, _ []uint8, sampleS float64, p SerialParams) decode.Result {
		return samples(a, sampleS, cfg(p))
	}}
	if edges != nil {
		d.Edges = func(a, _ *decode.EdgeLine, sampleS float64, p SerialParams) decode.Result {
			return edges(a, sampleS, cfg(p))
		}
	}
	return d
}

// clockedDecoder is a clocked two-line decoder, from samples or edges.
// TRLC-LINKS: REQ-SDS-013
func clockedDecoder(id int, name string,
	samples func(a, b []uint8, sampleS float64, cfg decode.ClockedCfg) decode.Result,
	edges func(a, b *decode.EdgeLine, sampleS float64, cfg decode.ClockedCfg) decode.Result,
) SerialDecoder {
	return SerialDecoder{
		ID: id, Name: name,
		Decode: func(a, b []uint8, sampleS float64, p SerialParams) decode.Result {
			return samples(a, b, sampleS, clockedCfg(p))
		},
		Edges: func(a, b *decode.EdgeLine, sampleS float64, p SerialParams) decode.Result {
			return edges(a, b, sampleS, clockedCfg(p))
		},
	}
}

// irCfg is a request's threshold as the infrared decoders take it.
// TRLC-LINKS: REQ-SDS-013
func irCfg(p SerialParams) decode.IRCfg {
	return decode.IRCfg{Threshold: p.Threshold, HaveThr: p.HaveThr}
}

// lineCfgFixed is lineCfg for a decoder that measures its own rate.
// TRLC-LINKS: REQ-SDS-013
func lineCfgFixed(p SerialParams) decode.LineCfg { return lineCfg(p, false) }

// lineCfgRate is lineCfg for a decoder that takes the request's rate.
// TRLC-LINKS: REQ-SDS-013
func lineCfgRate(p SerialParams) decode.LineCfg { return lineCfg(p, true) }

// clockedCfg is a request's two thresholds as the clocked decoders take them.
// TRLC-LINKS: REQ-SDS-013
func clockedCfg(p SerialParams) decode.ClockedCfg {
	return decode.ClockedCfg{Threshold: p.Threshold, HaveThr: p.HaveThr, ThresholdB: p.ThresholdB, HaveThrB: p.HaveThrB}
}

// lineCfg is a request's settings as the single-line decoders beyond the
// first thirty take them: Bits their sub-protocol, Baud the rate they would
// otherwise measure where rate says the decoder takes one (DCF77's scale,
// a Miller, EM4100 or SDQ bit rate, a reader's field frequency).
// TRLC-LINKS: REQ-SDS-013
func lineCfg(p SerialParams, rate bool) decode.LineCfg {
	c := decode.LineCfg{Threshold: p.Threshold, HaveThr: p.HaveThr, Variant: p.Bits}
	if rate {
		c.Baud = p.Baud
	}
	return c
}

// ClockedTwoLine reports the clocked two-line protocols beyond SPI and
// I2C: PS/2, SWD and MDIO (clock and data), Wiegand (D0 and D1), a
// quadrature encoder (A and B), a stepper (STEP and DIR), an SD card's CLK
// and CMD, PDI, cJTAG, Maple (SDCKA and SDCKB) and calipers.
// TRLC-LINKS: REQ-SDS-013
func ClockedTwoLine(proto int) bool {
	switch proto {
	case SerPS2, SerSWD, SerMDIO, SerWiegand, SerQuad, SerStepper, SerSDCard, SerPDI, SerCJTAG, SerMaple, SerCaliper:
		return true
	}
	return false
}
