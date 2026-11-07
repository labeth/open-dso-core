// Package render draws a scope's display: the waveform views, their readouts
// and the task-workspace UI, from a published frame and a HUD snapshot, into
// a pixel canvas. It never touches the acquisition hardware; a board presents
// the canvas on its panel and captures it for the web's screen view.
// ENGMODEL-OWNER-UNIT: FU-DSO-RENDER
package render
