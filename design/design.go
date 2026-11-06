// Package design is the look every scope shares: the colour tokens, the web
// stylesheets and the display font. A scope's web UI serves CSS from it and
// its display draws with Tokens and Glyphs, so the families look the same.
package design

import "embed"

//go:generate go run gen_tokens.go

// CSS holds the shared stylesheets, served by each scope's web UI at their
// own names: tokens.css (generated from tokens.json), base.css (the
// task-workspace layout), remote.css (the phone remote) and sound.css.
//
//go:embed *.css
var CSS embed.FS

// RGB is a token's colour, 8 bits per channel. A display converts it to its
// own pixel format.
type RGB struct{ R, G, B uint8 }
