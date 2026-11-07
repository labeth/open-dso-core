// Package engine is the scope-independent part of the acquisition engine:
// what the owner loop decides on frames, with no hardware access. It holds
// the frame arena between the owner and its consumers, the acquisition
// modes (average, ERES, the uniformity ring), the software trigger
// qualifiers and the placement of a hardware trigger on its crossing, the
// serial trigger's decoder registry, matchers and learned levels, zones and
// mask testing, the frequency-response measurement, the sound ring and its
// takes, the client-decode word transport, the detail windows beside a live
// envelope, and the owner loop's polling and AUTO policies. A scope's board
// package drives the hardware through the HAL; the owner loop composes the
// two.
package engine
