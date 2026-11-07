// Package hal is the board interface of the open oscilloscope family: what a
// scope repository implements so that the core can run the whole product on
// it. It holds only types and interfaces and imports only the standard
// library and the core's leaf package sample.
//
// # Threading
//
// Every method carries one mark saying where it may be called from:
//
//	OWNER   the core engine's owner goroutine only; never concurrently with
//	        another OWNER call; may touch hardware
//	COMMIT  OWNER, and only inside the commit step: setters stage, and the
//	        hardware sees them at the next Acquirer.Arm
//	LONG    started by the owner on its own goroutine with acquisition
//	        paused; the UI stays live and ctx cancels it
//	RENDER  the render goroutine only
//	ANY     any goroutine; never blocks on hardware; returns snapshots
//
// The owner's cycle, which every board can rely on:
//
//	loop:
//	  Platform.Service()            board housekeeping (key matrix, lamp word)
//	  commit staged state, in this order:
//	    FrontEnd.Set                vertical first: trigger levels depend on it
//	    Acquirer.Configure(plan)
//	    Trigger.Program(spec)       re-staged after every FrontEnd change
//	  Acquirer.Arm()                the one hardware commit
//	  Acquirer.Wait(ctx, timeout)   a ready record is halted; ErrTimeout is not a fault
//	  Readout.Read(req, dst, yield) only between a halt and the next Arm; skipped
//	                                when no consumer needs the frame
//	  publish
//
// Setters stage and Arm commits because one board writes the plan and every
// trigger in one register struct while others may reprogram only while
// stopped, and trigger levels are volts that need the vertical setting.
// Readout calls yield between chunks of a long read; the core runs
// Platform.Service there, so a board's panel shares the bus with the owner
// instead of running its own goroutine on it. The one exception is a
// running stream's transfer goroutine (see Streamer).
//
// Acquirer.Stop is the user's STOP, not a step of the cycle. Arm is one
// call for two kinds of arm: the board runs a commit arm (start delay,
// pending calibration) when something staged differs from what is applied
// or after Stop, and the plain re-arm otherwise.
//
// A frame's Scale is snapshotted into each trace at readout and never read
// back from the FrontEnd.
//
// # Errors
//
// A request a board cannot serve is a *Refusal carrying the real reason,
// found with errors.As. Nothing is substituted for a refused request: a
// board that cannot run a trigger node does not run another one instead.
// Acquirer.Wait's timeout is ErrTimeout. Platform.BringUp's ErrNotTaken
// means the unit stays with the vendor firmware and the app exits. Any
// other error is a fault the engine counts and logs.
//
// # Units
//
// Controllers speak volts and seconds. DAC codes, detent indexes and pixel
// levels exist only inside a board; knob steps come from Trigger.LevelGrid
// and FrontEnd.OffsetGrid. Signal time is float64 seconds; time.Duration is
// used only for wall-clock timeouts and costs.
package hal
