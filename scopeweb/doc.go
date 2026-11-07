// Package scopeweb is webserver.Scope implemented once, over the core: the
// HAL board (its profile, front end, trigger, storage and extensions), the
// instrument model every controller stages through, the engine's frame
// fan-out and the core engine's parts a scope runs (zone and mask tests,
// the protocol trigger, Bode, SOUND's stream, the client decode). A scope
// app builds a Config and mounts webserver.New(scopeweb.New(cfg)); the
// shared page then runs against it, whatever its channels and code format.
//
// Every part has a shared implementation. A scope whose engine still holds
// settings or state outside the instrument model (run control kept in its
// own flags, a trigger level kept as a DAC code, a v1 status document of
// its own engine's counters) hands those to Config in place of the shared
// ones; each such hook says what it stands in for, so it goes when that
// state moves to the core.
// ENGMODEL-OWNER-UNIT: FU-DSO-SCOPEWEB
package scopeweb
