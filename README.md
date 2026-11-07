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
| `webserver/` | the web server every scope mounts: the shared client's pages and assets and every route of the API over `webserver.Scope`, which a scope's adapter implements (status, frames and settings, and optional parts by typed accessor) |
| `webserver/wire/` | what goes over the socket: the routes' reply and header types and the binary frame, record, sound and words encoders |
| `webserver/webtest/` | the server's conformance suite (`webtest.Run`), which each scope's adapter runs, and a fake scope |
| `webserver/shape/` | frame shaping for the page: the screen window, envelope resampling, held stretches, the coupling of precision words, the measurement cache |
| `uimodel/` | the UI model's contract shared by a scope's display, the web page and the phone remote: panel snapshot, settings sheets and rows, set results, references, SOUND settings |
| `streamwire/` | the decoded stream's transcript as served: the list window and the lines as data |
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

## Engineering model

`model/` is the core's engineering model for
[engineering-model-go](https://github.com/labeth/engineering-model-go): the requirements the core
implements (`REQ-DSO-*`), one functional unit per package (`FU-DSO-*`, named in each file's
`ENGMODEL-OWNER-UNIT`), the capabilities a scope composes (`CAP-DSO-*`, publication `public`) and the
family's decision log (`decisions.yml`). Every function names the requirements it serves in
`TRLC-LINKS`. The model is held to the product tier (`ADR-CORE-MODEL-TIER`); how its IDs relate to a
scope's is `ADR-CORE-MODEL-IDS`. CI (`.github/workflows/model.yml`) validates it with `engdoc` at the
pinned release, from `model/`:

```sh
cd model
engdoc --model engmod.yml --requirements requirements.yml \
  --design views.yml --code-root .. --lock check --out /tmp/ARCHITECTURE.adoc
```

A scope's model composes this one as the CUE module `github.com/labeth/open-dso-core/model@v0`
(`model/cue.mod/module.cue`). Until it is published to a registry, the scope resolves it from a
checkout next to its own through an `engmod.work.yml` above both repositories.

## Licence

MIT, see [LICENSE](LICENSE).
