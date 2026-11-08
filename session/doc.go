// Package session is the recorded-session differential harness: one JSON
// file per session holds what an operator does to a scope (front-panel keys
// and knobs by the UI model's names, web actions, the signals on the
// inputs) and checkpoints where the scope's state is captured through its
// public web API. The same session runs against any target: the real scope
// on the bench, the native demo, the WebAssembly demo in a browser or a
// scope's simulator; two runs are then compared field by field.
//
// A checkpoint's snapshot is normalized from the API every target serves
// (the panel status and the sheet model of the core UI model, the
// capabilities document, the frames): the workspace, view, timebase, each
// channel's vertical setting, the trigger, the open sheet and its row, the
// run state, the measurements, the packets decoded from the served record
// and the features the scope refuses with their reasons. Fields that do not
// depend on the signal must match exactly; fields that do match within the
// session's tolerances; a field a target cannot produce is marked as such
// with the reason, never left out.
//
// Signals are written abstractly ("UART 9600 8N1 'hello' on C1"). A demo
// sets them on its generator; a simulator through its bench; the real
// scope's bench maps them onto declared sources (a bench file), and the
// runner says what to wire or select, or skips a session no source serves.
package session
