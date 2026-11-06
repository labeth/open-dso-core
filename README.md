# open-dso-core

The hardware-independent core of the open oscilloscope firmwares: protocol decoders, signal
processing, measurements, analysis (eye/jitter, bus layers, compare), super-resolution, sound
analysis and the VXI-11 server. Each instrument has its own repository that builds its app on
this core; today that is [open-sds1000cml](https://github.com/labeth/open-sds1000cml).

| Package | What it holds |
|---|---|
| `decode/` | the protocol decoders and their autodetect |
| `dsp/` | sample kernels: min/max, the precision FIR that conditions decimated records, rounding (ARM assembly with Go fallbacks) |
| `measure/` | the automatic measurements |
| `analysis/` | eye and jitter, bus layers, compare and masks |
| `superres/` | super-resolution stacking |
| `sound/` | the sound analysis |
| `vxi11srv/` | the VXI-11 server |
| `webserver/` | the web server every scope mounts: the shared client's pages and assets and every route of the API over `webserver.Scope`, which a scope's adapter implements (status, frames and settings, and optional parts by typed accessor) |
| `webserver/wire/` | what goes over the socket: the routes' reply and header types and the binary frame, record, sound and words encoders |
| `webserver/webtest/` | the server's conformance suite (`webtest.Run`), which each scope's adapter runs, and a fake scope |
| `webserver/shape/` | frame shaping for the page: the screen window, envelope resampling, held stretches, the coupling of precision words, the measurement cache |
| `uimodel/` | the UI model's contract shared by a scope's display, the web page and the phone remote: panel snapshot, settings sheets and rows, set results, references, SOUND settings |
| `streamwire/` | the decoded stream's transcript as served: the list window and the lines as data |
| `design/` | the shared look: colour tokens (one source for web and displays), the web stylesheets, the 5×7 display font |
| `testenv/` | test helpers: toolchain gates, and the scope checkout the parity and RTL tests run against |

## Tests

`go test ./...` runs the unit tests on any host. The Go↔JS parity tests and the FPGA RTL tests
check this code against a scope's web UI and FPGA sources. They find the scope checkout through
`DSO_SCOPE_DIR`, or a sibling `open-sds1000cml` directory, and skip without one.

## Licence

MIT, see [LICENSE](LICENSE).
