// Package scpi is the instrument-control engine every scope shares: the
// IEEE 488.2 / SCPI-99 parser and dispatcher, the common commands, the
// status model and error queue, and the state binding the commands use.
//
// A scope supplies a Dialect: its grammar, its command table (patterns and
// handlers) and a few rules. Two grammars exist because instruments of one
// family speak both:
//
//   - IEEE: libscpi behaviour. Compound headers continue the previous path,
//     nodes match their short or long form, numeric suffixes default to 1,
//     errors go to a 16-entry queue, and replies are joined by ';' with one
//     LF after a message whose last unit wrote output.
//   - Line: the legacy short-form grammar. Each ';'-separated unit runs on
//     its own, upper-cased; a source prefix (C1:, C1-C2:) gives the
//     suffixes; every reply carries its own LF and an error is answered in
//     place with its standard text.
//
// Handlers bind to the instrument through the Instrument interface, in
// volts and seconds; they never touch hardware.
package scpi
