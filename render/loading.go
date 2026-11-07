// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import "strconv"

// DrawLoading displays an explicit acquisition interruption. Transfer progress
// is supplied by the loader; this never advances a simulated progress bar.
// TRLC-LINKS: REQ-SDS-021
func DrawLoading(sf Canvas, stage string, sent, total int) {
	sf.Fill(c565(0x0841))
	title := "Loading acquisition"
	footer := "Controls return when loading finishes"
	if stage == "Firmware loading failed" || stage == "Unable to read firmware" {
		title = "Acquisition unavailable"
		footer = "Firmware loading stopped"
	}
	DrawText(sf, (W-TextWidth(title, 3))/2, 165, title, c565(0xffff), 3)
	DrawText(sf, (W-TextWidth(stage, 2))/2, 220, stage, c565(0xbdf7), 2)
	if total > 0 {
		if sent < 0 {
			sent = 0
		}
		if sent > total {
			sent = total
		}
		done := 520 * sent / total
		fillSpan(sf, 140, 270, 140+done, 286, c565(0x07ff))
		fillSpan(sf, 140+done, 270, 660, 286, c565(0x3186))
		label := strconv.Itoa(100*sent/total) + "% transferred"
		DrawText(sf, (W-TextWidth(label, 2))/2, 308, label, c565(0xffff), 2)
	}
	DrawText(sf, (W-TextWidth(footer, 2))/2, 390, footer, c565(0x94b2), 2)
}
