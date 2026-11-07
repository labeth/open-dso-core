// ENGMODEL-OWNER-UNIT: FU-APP-LCD

package render

import "strconv"

// DrawLoading displays an explicit acquisition interruption. Transfer progress
// is supplied by the loader; this never advances a simulated progress bar.
// TRLC-LINKS: REQ-SDS-021
func DrawLoading(sf Canvas, stage string, sent, total int) {
	sm := metricsOf(sf)
	cx, cy := sm.W/2, sm.H/2 // the picture is centred: 400, 240 on 800×480
	sf.Fill(c565(0x0841))
	title := "Loading acquisition"
	footer := "Controls return when loading finishes"
	if stage == "Firmware loading failed" || stage == "Unable to read firmware" {
		title = "Acquisition unavailable"
		footer = "Firmware loading stopped"
	}
	DrawText(sf, cx-TextWidth(title, 3)/2, cy-75, title, c565(0xffff), 3)
	DrawText(sf, cx-TextWidth(stage, 2)/2, cy-20, stage, c565(0xbdf7), 2)
	if total > 0 {
		if sent < 0 {
			sent = 0
		}
		if sent > total {
			sent = total
		}
		done := 520 * sent / total
		x0 := cx - 260
		fillRect(sf, x0, cy+30, done, 16, c565(0x07ff))
		fillRect(sf, x0+done, cy+30, 520-done, 16, c565(0x3186))
		label := strconv.Itoa(100*sent/total) + "% transferred"
		DrawText(sf, cx-TextWidth(label, 2)/2, cy+68, label, c565(0xffff), 2)
	}
	DrawText(sf, cx-TextWidth(footer, 2)/2, cy+150, footer, c565(0x94b2), 2)
}
