// ENGMODEL-OWNER-UNIT: FU-DSO-WEBCLIENT
// The workflows' sources on the browser demo's signal generator: what the
// bench's FPGA board drives (README "Sources"), set through the simulated
// board's bench routes (/api/sim/…) instead of flashing a board. A source the
// generator cannot make is refused with the reason.

const msg = "48692055aa0ff00a"; // "Hi \x55\xAA\x0F\xF0\n", the protocol sources' message
// TRLC-LINKS: REQ-DSO-298
const quiet = (n) => Object.fromEntries(Array.from({ length: n }, (_, i) => [`C${i + 1}`, "dc,0,0,0"]));

// SOURCES maps a source to its generator setting: signals per input, or a bus.
export const SOURCES = {
  tone1M: { signals: { C1: "square,1e6,3.3,1.65" } },
  prbs2M: { bus: { kind: "prbs", lines: { data: "C1", clk: "C2" }, rate: 2e6, vpp: 3.3, offset: 1.65, opts: { order: "7" } } },
  spi: { bus: { kind: "spi", lines: { clk: "C1", mosi: "C2" }, rate: 200e3, vpp: 3.3, offset: 1.65, opts: { data: msg } } },
  uart: { signals: { C1: `uart,115200,3.3,1.65,data=${msg}`, C2: `uart,115200,3.3,1.65,data=${msg}` } },
  maskviol: { signals: { C1: "pulse,2500,3.3,1.65,width=100e-6,stretch=7:12e-6", C2: "dc,0,0,0" } },
  burst: { refused: "the generator makes no frequency-stepped burst (50/150/250 MHz tones in one 300 ns pattern)" },
};

// applySource sets source src on the demo's generator through page (any page
// of the demo's origin); it returns "" or the reason the source is refused.
// TRLC-LINKS: REQ-DSO-180, REQ-DSO-295
export async function applySource(page, src) {
  const s = SOURCES[src];
  if (!s) return `no generator setting for source ${src}`;
  if (s.refused) return s.refused;
  return page.evaluate(async ({ s, quietAll }) => {
    // TRLC-LINKS: REQ-DSO-298
    const post = async (p, body) => (await fetch(p, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })).json();
    const st = await (await fetch("/api/sim/state")).json();
    const n = st.channels || 2;
    const all = Object.fromEntries(Object.entries(quietAll).filter(([k]) => +k.slice(1) <= n));
    let r = await post("/api/sim/signal", { signals: { ...all, ...(s.signals || {}) } });
    if (r.ok && s.bus) r = await post("/api/sim/bus", s.bus);
    return r.ok ? "" : r.err || "the generator refused the source";
  }, { s, quietAll: quiet(8) });
}
