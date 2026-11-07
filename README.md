# open-dso-core

The hardware-independent core of the open oscilloscope firmwares: protocol decoders, signal
processing, measurements, analysis (eye/jitter, bus layers, compare), super-resolution, sound
analysis and the VXI-11 server. Each instrument has its own repository that builds its app on
this core; today that is [open-sds1000cml](https://github.com/labeth/open-sds1000cml).

| Package | What it holds |
|---|---|
| `hal/` | the board interface: what a scope implements so the core runs on it; `hal/halfake` is a fake board for tests |
| `sample/` | the published frame: traces of 8- or 16-bit codes with their code format, content kind and scale |
| `profile/` | reads a scope's `profile.yaml` into a `hal.Profile`, variant by unit identity |
| `engine/fanout/` | hands the engine's newest frame to every reader (display, web) |
| `decode/` | the protocol decoders and their autodetect |
| `dsp/` | sample kernels: min/max, the precision FIR that conditions decimated records, rounding (ARM assembly with Go fallbacks) |
| `measure/` | the automatic measurements |
| `analysis/` | eye and jitter, bus layers, compare and masks |
| `superres/` | super-resolution stacking |
| `sound/` | the sound analysis |
| `vxi11srv/` | the VXI-11 server |
| `design/` | the shared look: colour tokens (one source for web and displays), the web stylesheets, the 5×7 display font |
| `testenv/` | test helpers: toolchain gates, and the scope checkout the parity and RTL tests run against |

`supervisor/` is a second Go module (`github.com/labeth/open-dso-core/supervisor`), kept apart so
the app-side module never lists NATS: the on-device update supervisor (`agent`: A/B slots, health
rollback, the RPC on TCP and NATS, the takeover's crash-safe order), `watchdog` (the keeper),
`otactl` (the host controller, library and `cmd/otactl`), `diag` (the diagnostics stick's
framework), and `config`, `slots`, `rpcproto`, `fdinherit`, `sysinfo`, `vxi11`. A scope's `ota/`
module implements `agent.Board` (`hal.SupervisorBoard` and its takeover's landing) and
`diag.Board`; `agent/agenttest` is a board for tests.

## Tests

`go test ./...` runs the unit tests on any host. The Go↔JS parity tests and the FPGA RTL tests
check this code against a scope's web UI and FPGA sources. They find the scope checkout through
`DSO_SCOPE_DIR`, or a sibling `open-sds1000cml` directory, and skip without one.

## Licence

MIT, see [LICENSE](LICENSE).
