// Package shape turns a scope's published record into what the web page
// draws: the screen window, envelope resampling, held stretches, the
// coupling transform of precision words, and the measurement cache shared by
// every reply of one frame. It is pure: no locks but the cache's, no scope.
// ENGMODEL-OWNER-UNIT: FU-APP-WEB
package shape
