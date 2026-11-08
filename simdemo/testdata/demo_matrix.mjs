// The browser demo's test matrix: drives a built demo site (the firmware as
// WebAssembly in a headless Chromium tab, or the native demo server's bench
// page) the way an operator drives the scope: the generator on the bench,
// the front panel's keys, the scope's own web UI. Each case reads what the
// scope shows (its notices, its web page, its lamps) and says what failed.
// A feature the simulated board refuses must say why on screen.
//
//   node demo_matrix.mjs BENCH_URL [UI_URL]
//
// BENCH_URL is the bench page (a site's index.html, or the native server's
// /demo/); UI_URL the web UI (default: ui/ beside the bench page, or the
// native server's root). CASES (a regular expression) picks cases; SHOTS a
// directory for screenshots of failures. Prints one JSON line per case and
// a summary line {"summary":…}; exits 1 when a case fails.
import fs from "node:fs";
import path from "node:path";
import { findPlaywright } from "../../webclient/scope_po.mjs";

const benchURL = process.argv[2];
const pw = findPlaywright();
if (!benchURL || !pw) {
  console.log(JSON.stringify({ skip: !pw ? "playwright not installed" : "no URL" }));
  process.exit(0);
}
const native = /\/demo\/?$/.test(new URL(benchURL).pathname);
const uiURL = process.argv[3] || (native ? new URL("/", benchURL).href : new URL("ui/", benchURL).href);
const only = process.env.CASES ? new RegExp(process.env.CASES) : null;
const shots = process.env.SHOTS || "";
if (!process.env.PLAYWRIGHT_BROWSERS_PATH && process.env.HOME)
  process.env.PLAYWRIGHT_BROWSERS_PATH = path.join(process.env.HOME, ".cache/ms-playwright");
const { chromium } = (await import(pw)).default;
const browser = await chromium.launch({ headless: true, args: ["--no-sandbox", "--use-gl=egl"] });
const ctx = await browser.newContext({ viewport: { width: 1500, height: 1000 } });
const bench = await ctx.newPage();
const errors = [];
bench.on("pageerror", (e) => errors.push(`bench: ${e.message}`));
const t0 = Date.now();
await bench.goto(benchURL, { waitUntil: "load", timeout: 60000 });
await bench.waitForFunction(() => window.demoStats && (window.demoStats.firstFrameMs > 0 || window.demoStats.pictures > 0), null, { timeout: 120000 });
const loadMs = Date.now() - t0;
const ui = await ctx.newPage();
ui.on("pageerror", (e) => errors.push(`web UI: ${e.message}`));
// the web UI's first frame; a tab opened while the service worker still
// takes the engine's port is loaded again once
for (let tries = 0; ; tries++) {
  await ui.goto(uiURL, { waitUntil: "domcontentloaded", timeout: 60000 });
  try {
    await ui.waitForFunction(() => typeof frame !== "undefined" && frame && frame.seq > 0, null, { timeout: 60000 });
    break;
  } catch (e) {
    if (tries > 0) throw e;
  }
}

// ---------------------------------------------------------------- the bench's hands
// TRLC-LINKS: REQ-DSO-298
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// TRLC-LINKS: REQ-DSO-298
const api = (p, body) => bench.evaluate(async ([p, body]) => {
  const r = await fetch(p, body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  return r.json().catch(() => ({ ok: false, err: r.statusText }));
}, [p, body]);
// TRLC-LINKS: REQ-DSO-298
const status = () => api("/api/status");
// TRLC-LINKS: REQ-DSO-298
const panel = async () => (await status()).panel || {};
// TRLC-LINKS: REQ-DSO-298
const simState = () => api("/api/sim/state");
// key presses a front-panel key on the bench page, as a hand does
// TRLC-LINKS: REQ-DSO-298
const key = async (k) => {
  const b = await bench.$(`button[data-key="${k}"]`);
  if (b) await b.click(); else await api("/api/sim/input", { key: k });
  await sleep(250);
};
// TRLC-LINKS: REQ-DSO-298
const knob = (k, steps) => api("/api/sim/input", { knob: k, steps });
let nch = 2;
// TRLC-LINKS: REQ-DSO-298
const quietAll = () => Object.fromEntries(Array.from({ length: nch }, (_, i) => [`C${i + 1}`, "dc,0,0,0"]));
// TRLC-LINKS: REQ-DSO-298
const signals = async (s) => {
  const r = await api("/api/sim/signal", { signals: { ...quietAll(), ...s } });
  if (!r.ok) throw new Error(`generator: ${r.err}`);
};
// TRLC-LINKS: REQ-DSO-298
const busOn = async (b) => {
  await signals({});
  const r = await api("/api/sim/bus", b);
  if (!r.ok) throw new Error(`generator bus: ${r.err}`);
};
// waitFor polls fn until it returns a truthy value or ms pass (then the last value)
// TRLC-LINKS: REQ-DSO-298
async function waitFor(fn, ms, every = 300) {
  const end = Date.now() + ms;
  let v;
  for (;;) {
    v = await fn();
    if (v || Date.now() > end) return v;
    await sleep(every);
  }
}
// workspace switches the scope's workspace on its function keys: the F-key
// whose label names it, as the LCD's workspace bar shows (the remote's
// workspace request where the bar is not up, or the panel has no F-keys: a
// touch screen's workspace bar is touched, not pressed).
const wsLabel = { scope: "SCOP", bus: "BUS", spectrum: "SPEC", measure: "MEAS", compare: "COMP" };
// TRLC-LINKS: REQ-DSO-298
async function workspace(id) {
  const p = await panel();
  if (p.WorkspaceID === id) return;
  const i = p.Mode === "workspace" ? (p.FKeys || []).findIndex((f) => f.Label === wsLabel[id]) : -1;
  if (i >= 0 && (await bench.$(`button[data-key="f${i + 1}"]`))) await key(`f${i + 1}`);
  else await api("/api/panel", { workspace: id });
  await waitFor(async () => (await panel()).WorkspaceID === id, 5000);
}
// idle waits for AUTO, a detection and an autoset to end
// TRLC-LINKS: REQ-DSO-298
const idle = (ms = 40000) => waitFor(async () => {
  const p = await panel();
  return !p.AutosetBusy && !(p.Display && p.Display.DecDetecting);
}, ms, 400);
// TRLC-LINKS: REQ-DSO-298
async function defaultSetup() {
  await workspace("scope");
  await key("defaultsetup");
  await sleep(800);
  await idle();
}
// auto presses AUTO and waits for it to end; it returns the notice it left
// TRLC-LINKS: REQ-DSO-298
async function auto() {
  const before = (await panel()).AutosetDone;
  await key("auto");
  await waitFor(async () => {
    const p = await panel();
    return p.AutosetDone !== before && !p.AutosetBusy && !(p.Display && p.Display.DecDetecting);
  }, 60000, 400);
  await sleep(1200);
  return panel();
}
// webText reads the web UI's elements' text (a textarea's value)
// TRLC-LINKS: REQ-DSO-298
const webText = (sel) => ui.evaluate((s) => [...document.querySelectorAll(s)].map((e) => (e.value !== undefined && e.tagName === "TEXTAREA" ? e.value : e.textContent)).join("\n"), sel);
// TRLC-LINKS: REQ-DSO-298
const webClick = async (sel) => { await ui.click(sel); await sleep(300); };
// TRLC-LINKS: REQ-DSO-298
const hexOf = (s) => s.match(/../g).join(" ").toUpperCase();
// TRLC-LINKS: REQ-DSO-298
const lamps = async () => (await simState()).lamps || [];

// ---------------------------------------------------------------- the cases
const cases = [];
// TRLC-LINKS: REQ-DSO-298
const add = (id, fn) => cases.push({ id, fn });

// The owner's flow: the generator's UART preset on C1, C2 off, BUS on the
// front panel, AUTO: the scope finds the UART and decodes "Hello".
add("owner-uart-hello", async () => {
  await defaultSetup();
  await bench.click('#gentabs button:has-text("Protocol")');
  await bench.click('.presets button:has-text("UART")');
  await sleep(500);
  await bench.click('#gentabs button:has-text("C2")');
  await bench.click("#w-off");
  await sleep(500);
  await workspace("bus");
  await idle();
  const p = await auto();
  const note = p.Display.DecNote || "";
  if (!/^UART 115200/.test(note)) throw new Error(`BUS AUTO: "${note}"`);
  await webClick("#tab-bus");
  const rows = await waitFor(async () => { const t = await webText("#pkBody tr"); return /Hello/.test(t) ? t : ""; }, 15000);
  if (!rows) throw new Error(`the web UI's packets never read "Hello": ${(await webText("#pkBody tr")).slice(0, 120)}`);
  return { note, trigger: (await status()).trig };
});

// AUTO in BUS on every bus the generator makes: the bus named, its payload
// decoded as the generator sends it, the trigger on its frame start.
const buses = [
  { id: "uart", name: /^UART 115200/, bus: { kind: "uart", lines: { tx: "C1" }, rate: 115200, vpp: 3.3, offset: 1.65, opts: { data: "48656c6c6f" } }, hex: "48656c6c6f" },
  { id: "spi", name: /^SPI mode 0/, bus: { kind: "spi", lines: { clk: "C1", mosi: "C2" }, rate: 1e6, vpp: 3.3, offset: 1.65, opts: { data: "a53c" } }, hex: "a53c" },
  { id: "i2c", name: /^I2C/, bus: { kind: "i2c", lines: { scl: "C1", sda: "C2" }, rate: 100e3, vpp: 3.3, offset: 1.65, opts: { data: "00a5" } }, hex: "00a5" },
  { id: "can-hl", name: /^CAN 500kb\/s/, bus: { kind: "can", lines: { h: "C1", l: "C2" }, rate: 500e3, vpp: 2, offset: 2.5, opts: { data: "deadbeef" } }, hex: "deadbeef" },
  { id: "can-rx", name: /^CAN 500kb\/s/, bus: { kind: "can", lines: { rx: "C1" }, rate: 500e3, vpp: 3.3, offset: 1.65, opts: { data: "deadbeef" } }, hex: "deadbeef" },
  { id: "lin", name: /^LIN 19200/, bus: { kind: "lin", lines: { tx: "C1" }, rate: 19200, vpp: 12, offset: 6, opts: { data: "0102" } }, hex: "0102" },
  { id: "manchester", name: /^Manch 1Mb\/s/, bus: { kind: "manchester", lines: { tx: "C1" }, rate: 1e6, vpp: 3.3, offset: 1.65, opts: { data: "a5f0" } }, hex: "a5f0" },
  // a PRBS and its bit clock read as a clocked bus (SPI, or an SD card's
  // clock and command line): any bus AUTO names and triggers on
  { id: "prbs-clock", name: /^(?!no bus)\S/, bus: { kind: "prbs", lines: { data: "C1", clk: "C2" }, rate: 2e6, vpp: 3.3, offset: 1.65, opts: { order: "7" } } },
];
for (const b of buses) {
  add(`bus-auto-${b.id}`, async () => {
    await defaultSetup();
    await busOn(b.bus);
    await workspace("bus");
    await idle();
    const p = await auto();
    const note = p.Display.DecNote || "";
    if (!b.name.test(note)) throw new Error(`BUS AUTO named "${note}"`);
    const st = await status();
    const trig = `${st.trig && st.trig.type} ${st.serial_backend || st.trigger_backend || ""}`.trim();
    if (/none:/.test(trig)) throw new Error(`the frame-start trigger is not decided: ${trig}`);
    let rows = "";
    if (b.hex) {
      await webClick("#tab-bus");
      const want = hexOf(b.hex);
      rows = await waitFor(async () => { const t = (await webText("#pkBody tr")).toUpperCase().replace(/\s+/g, " "); return t.includes(want) ? t : ""; }, 15000);
      if (!rows) throw new Error(`the packets never held ${want}: ${(await webText("#pkBody tr")).slice(0, 160)}`);
    }
    return { note, trigger: trig, runState: st.run_state, tdiv: st.tdiv_set_s };
  });
}

// BUS's views on the UART: Packets, Eye, Stream and Visual, each with
// content or the reason it has none.
add("bus-views", async () => {
  await defaultSetup();
  await busOn(buses[0].bus);
  await workspace("bus");
  await idle();
  await auto();
  const seen = {};
  for (let i = 0; i < 4; i++) {
    const p = await panel();
    await sleep(2500);
    const q = await panel();
    seen[q.View] = { notice: q.Notice || "", caption: (q.Display && q.Display.DecNote) || "" };
    await api("/api/panel", { again: true });
    await sleep(800);
    if (p.View === (await panel()).View) throw new Error(`F2 again left the view on ${p.View}`);
  }
  // the web's views: the eye folds full-rate captures and reads an eye height
  await webClick("#tab-bus");
  await webClick('#busView button[data-value="eye"]');
  if (!(await ui.evaluate(() => document.getElementById("ejArm").classList.contains("on")))) await webClick("#ejArm");
  // TRLC-LINKS: REQ-DSO-298
  const height = async () => { const t = await webText("#ejBody"); return /eye height\s*[\d.]/.test(t) ? t : ""; };
  let eye = await waitFor(height, 20000);
  if (!eye) { // a timebase no record holds at the full rate: Optimize steps to one that does, as an operator would
    await webClick("#ejOpt");
    eye = await waitFor(height, 60000);
  }
  if (!eye) throw new Error(`the web's eye never measured: ${await webText("#ejStats")}`);
  await webClick('#busView button[data-value="stream"]');
  await sleep(2500);
  const stream = (await webText("#ws-bus")).replace(/\s+/g, " ");
  await webClick('#busView button[data-value="visual"]');
  await sleep(1500);
  await webClick('#busView button[data-value="packets"]');
  return { views: seen, eye: eye.replace(/\s+/g, " ").slice(0, 160), stream: stream.slice(0, 200) };
});

// The timebase across every workspace switch: what each workspace leaves
// it at, and the notice that says why when it moves.
add("workspace-tdiv", async () => {
  await defaultSetup();
  await signals({ C1: "uart,115200,3.3,1.65,data=48656c6c6f" });
  const order = ["scope", "bus", "spectrum", "measure", "compare", "scope", "bus", "scope"];
  const trail = [];
  let prev = (await status()).tdiv_set_s;
  for (const w of order) {
    await workspace(w);
    await sleep(1500);
    await idle();
    const st = await status();
    const p = st.panel;
    const moved = Math.abs(st.tdiv_set_s - prev) > 1e-3 * prev;
    trail.push({ ws: w, tdiv: st.tdiv_set_s, moved, notice: p.Notice || "", bus: (p.Display && p.Display.DecNote) || "" });
    if (moved && !/\/div/.test(`${p.Notice} ${p.Display && p.Display.DecNote}`)) throw new Error(`${w} moved the timebase to ${st.tdiv_set_s} s/div and said nothing: ${JSON.stringify(trail)}`);
    prev = st.tdiv_set_s;
  }
  return { trail };
});

// SCOPE's AUTO on a square, then MEASURE's frequency on the web UI.
add("scope-auto-measure", async () => {
  await defaultSetup();
  await signals({ C1: "square,1e3,3,1.5" });
  const p = await auto();
  await workspace("measure");
  await webClick("#tab-measure");
  const f = await waitFor(async () => { const t = await webText('[data-meas="1:Freq"]'); return /\d/.test(t) ? t : ""; }, 10000);
  if (!f) throw new Error(`MEASURE's frequency never read: ${(await webText("#measBody")).slice(0, 160)}`);
  return { notice: p.Notice, freq: f };
});

// SCOPE's AUTO on a UART line: Freq reads, as on the scope.
add("uart-autoset-freq", async () => {
  await defaultSetup();
  await signals({ C1: "uart,115200,3.3,1.65,data=48692055aa0ff00a" });
  const p = await auto();
  await webClick("#tab-scope");
  const f = await waitFor(async () => { const t = await webText('[data-meas="1:Freq"]'); return /\d/.test(t) ? t : ""; }, 10000);
  if (!f) throw new Error(`Freq on C1 never read after AUTO ("${p.Notice}")`);
  return { notice: p.Notice, freq: f };
});

// SPECTRUM: the FFT's top peak at the generator's frequency; SOUND works or
// says why.
add("spectrum-fft-sound", async () => {
  await defaultSetup();
  await signals({ C1: "sine,2.5e3,2,0" });
  await workspace("spectrum");
  const p = await auto();
  await webClick("#tab-spectrum");
  const peaks = await waitFor(async () => { const t = await webText("#fftBody, #pkBody2, #peaksBody"); return /2\.50*\s*kHz/.test(t) ? t : ""; }, 15000);
  if (!peaks) throw new Error(`no peak at 2.5 kHz: ${(await webText("#fftBody, #peaksBody")).replace(/\s+/g, " ").slice(0, 160)}`);
  await webClick("#tab-sound");
  await sleep(3000);
  const sound = (await webText("#sndStatus")).trim();
  if (/HTTP \d/.test(sound)) throw new Error(`SOUND says only "${sound}"`);
  return { notice: p.Notice, peaks: peaks.replace(/\s+/g, " ").slice(0, 160), sound };
});

// reveal brings a web control on screen as an operator does (its
// workspace, pane and view) and clicks it.
// TRLC-LINKS: REQ-DSO-298
const reveal = async (id) => {
  await ui.evaluate((i) => { const e = document.getElementById(i); if (e && typeof revealControl === "function") revealControl(e); }, id);
  await sleep(400);
  await ui.click(`#${id}`);
  await sleep(300);
};

// COMPARE: the stacked master and a mask test, or the reason there is none.
add("compare-master-mask", async () => {
  await defaultSetup();
  await signals({ C1: process.env.MASTER_SIG || "pulse,2500,3.3,1.65,width=100e-6" });
  await workspace("compare");
  const p = await auto();
  await webClick("#tab-compare");
  await reveal("masterBtn");
  const master = await waitFor(async () => { const t = await webText("#masterStat"); return t.trim() && !/averaging/.test(t) ? t : ""; }, 40000);
  if (!master || !/master/i.test(master)) throw new Error(`the master never settled: ${await webText("#masterStat")}`);
  await reveal("zmBuild");
  const mask = await waitFor(async () => { const t = await webText("#zmStats"); return /built|not available|failed/.test(t) ? t : ""; }, 30000);
  if (!mask) throw new Error(`the mask build said nothing: ${await webText("#zmStats")}`);
  if (/failed/.test(mask)) throw new Error(`the mask build failed: ${mask}`);
  return { notice: p.Notice, master: master.trim(), mask: mask.trim() };
});

// Super-resolution: stacks raw records of a repeating feature, or says why.
add("superres", async () => {
  await defaultSetup();
  await signals({ C1: "square,1e6,3.3,1.65,rise=2e-9" });
  await workspace("scope");
  await auto();
  await reveal("srAutoGate");
  await reveal("srArm");
  // a stack of records: "N hits · M fr" with M growing
  // TRLC-LINKS: REQ-DSO-298
  const frames = (t) => +((/(\d+) fr\b/.exec(t) || [])[1] || 0);
  const stat = await waitFor(async () => { const t = await webText("#srStats"); return /unsupported|failed/.test(t) || frames(t) >= 8 ? t : ""; }, 40000);
  await reveal("srArm");
  if (!stat || /unsupported|failed/.test(stat)) throw new Error(`super-res: ${stat || (await webText("#srStats"))}`);
  return { stats: stat.trim().slice(0, 200) };
});

// SYS: the firmware's name on the SYS tab and the web's title.
add("sys-identity", async () => {
  const caps = await api("/api/caps");
  const title = await ui.title();
  const want = caps.identity && caps.identity.model;
  if (!want || !title.includes(want)) throw new Error(`web title "${title}" does not name ${want}`);
  return { title, identity: caps.identity };
});

// Channel keys: an off channel's key turns it on with its lamp, another's
// key selects that channel, a channel turned off on its tab goes dark.
add("channel-keys", async () => {
  await defaultSetup();
  const log = [];
  // TRLC-LINKS: REQ-DSO-298
  const on = async (c) => (await status())[`on${c}`];
  await api("/api/panel", { set: { row: "ch2.on", text: "Off" } });
  await sleep(800);
  log.push({ step: "C2 off on its tab", on2: await on(2), lamps: await lamps() });
  if (await on(2) || (await lamps()).includes("ch2")) throw new Error(`C2 off: ${JSON.stringify(log)}`);
  await key("ch2");
  await sleep(800);
  log.push({ step: "C2 key", on2: await on(2), lamps: await lamps(), tab: (await panel()).Sheet.Tab });
  if (!(await on(2)) || !(await lamps()).includes("ch2")) throw new Error(`the C2 key did not turn C2 on: ${JSON.stringify(log)}`);
  await key("ch1");
  await sleep(500);
  const tab = (await panel()).Sheet.Tab;
  log.push({ step: "C1 key", tab });
  if (!/1/.test(tab)) throw new Error(`the C1 key did not select C1: ${JSON.stringify(log)}`);
  await key("ch1");
  await sleep(500);
  log.push({ step: "C1 key again", open: (await panel()).Sheet.Open, on1: await on(1) });
  return { log };
});

// ---------------------------------------------------------------- touch
// A scope with a touch screen and no F-keys lays its LCD out for a finger
// (the core's touch geometry, render/touch.go): a readout bar on top, the
// touch bar at the bottom, a sheet as a panel on the right. The hand here
// is the mouse on the bench page's screen, as demo.js passes it on: a click
// is a tap, a drag a finger moving, Shift+drag two fingers, Ctrl+wheel a
// pinch. The CML has no touch screen: these cases say so and pass.
// TRLC-LINKS: REQ-DSO-299
const touchFirst = async () => {
  const st = await simState();
  return st.screen && st.screen.touch && !(st.keys || []).some((k) => /^f[1-5]$/.test(k));
};
// TRLC-LINKS: REQ-DSO-299
const TW = 1024, TH = 600, BAR_Y = TH - 28, ACT_X = TW - 4 - 5 * 64, VIEWS_X = 4 + 5 * 80 + 10;
// TRLC-LINKS: REQ-DSO-299
const screenAt = async (x, y) => {
  const b = await bench.locator("#screen").boundingBox();
  return [b.x + (x + 0.5) * b.width / TW, b.y + (y + 0.5) * b.height / TH];
};
// TRLC-LINKS: REQ-DSO-299
const tap = async (x, y) => { const [px, py] = await screenAt(x, y); await bench.mouse.click(px, py); await sleep(350); };
// tapUntil taps until cond holds, twice at most (a busy WebAssembly panel
// may drop a click's events, as a real panel's full queue does); it
// returns cond's last value
// TRLC-LINKS: REQ-DSO-299
const tapUntil = async (x, y, cond) => {
  let v;
  for (let k = 0; k < 2 && !v; k++) { await tap(x, y); v = await waitFor(cond, 4000); }
  return v;
};
// TRLC-LINKS: REQ-DSO-299
const dragOn = async (x0, y0, x1, y1, mod) => {
  const [a, b] = await screenAt(x0, y0), [c, d] = await screenAt(x1, y1);
  if (mod) await bench.keyboard.down(mod);
  await bench.mouse.move(a, b);
  await bench.mouse.down();
  for (let k = 1; k <= 8; k++) { await bench.mouse.move(a + (c - a) * k / 8, b + (d - b) * k / 8); await sleep(50); }
  await bench.mouse.up();
  if (mod) await bench.keyboard.up(mod);
  await sleep(600);
};
// TRLC-LINKS: REQ-DSO-299
const pinchAt = async (x, y, dir) => {
  const [a, b] = await screenAt(x, y);
  await bench.mouse.move(a, b);
  await bench.keyboard.down("Control");
  await bench.mouse.wheel(0, dir > 0 ? -100 : 100);
  await bench.keyboard.up("Control");
  await sleep(700);
};
// TRLC-LINKS: REQ-DSO-299
const sheets = () => api("/api/sheets");
// TRLC-LINKS: REQ-DSO-299
const rowOf = async (id) => { for (const t of (await sheets()).Tabs || []) for (const r of t.Rows) if (r.ID === id) return r; return null; };
// the rows a gesture must leave alone: the trigger's, the channels', the
// timebase's and the acquisition's, all but the view's zoom and its pan
// TRLC-LINKS: REQ-DSO-299
const settingRows = async () => {
  const out = [];
  for (const t of (await sheets()).Tabs || []) {
    if (!/^(trig|time|acq|ch\d)$/.test(t.ID)) continue;
    for (const r of t.Rows) if (r.Kind !== "action" && r.ID !== "time.zoom" && r.ID !== "time.zoom_pos") out.push(`${r.ID}=${JSON.stringify(r.Value)}`);
  }
  return out.join(" ");
};
// rowY is the middle row of sheet row id on its tab's panel (opened at its
// first row): each section header above a row takes 24 px, a row 48.
// TRLC-LINKS: REQ-DSO-299
const rowY = async (tab, id) => {
  for (const t of (await sheets()).Tabs || []) {
    if (t.ID !== tab) continue;
    let y = 44;
    for (const r of t.Rows) {
      if (r.Section) y += 24;
      if (r.ID === id) return y + 24;
      y += 48;
    }
  }
  throw new Error(`no row ${id} on ${tab}`);
};
// TRLC-LINKS: REQ-DSO-299
const zoomNow = async () => ((await rowOf("time.zoom")) || {}).Value;

// The workspaces and their views on the touch bar.
add("touch-workspaces-views", async () => {
  if (!(await touchFirst())) return { skip: "no touch-first screen" };
  await defaultSetup();
  const ids = ["scope", "bus", "spectrum", "measure", "compare"], views = [2, 4, 4, 2, 0];
  const log = [];
  for (let i = 0; i < 5; i++) {
    const p = await tapUntil(4 + i * 80 + 40, BAR_Y, async () => { const q = await panel(); return q.WorkspaceID === ids[i] ? q : null; });
    if (!p) throw new Error(`the ${ids[i]} tab: workspace ${(await panel()).WorkspaceID}`);
    const n = views[i];
    if (!n) continue;
    const w = Math.min(120, Math.floor((ACT_X - 10 - VIEWS_X) / n));
    const v0 = p.View;
    const v1 = await tapUntil(VIEWS_X + (n - 1) * w + w / 2, BAR_Y, async () => { const q = await panel(); return q.View !== v0 ? q.View : null; });
    if (!v1) throw new Error(`${ids[i]}: the last view segment left the view on ${v0}`);
    if (!(await tapUntil(VIEWS_X + w / 2, BAR_Y, async () => (await panel()).View === v0))) throw new Error(`${ids[i]}: the first view segment did not bring ${v0} back`);
    log.push(`${ids[i]}: ${v0} <-> ${v1}`);
  }
  await tap(4 + 40, BAR_Y);
  return { log };
});

// The readouts open their sheets; a row's controls set the scope; Close
// and a tap beside the panel close it.
add("touch-readouts-sheets", async () => {
  if (!(await touchFirst())) return { skip: "no touch-first screen" };
  await defaultSetup();
  const chips = [[4 + 50, "ch1"], [4 + nch * 104 + 60, "time"], [4 + nch * 104 + 124 + 95, "trig"]];
  for (const [x, tab] of chips) {
    await tap(x, 20);
    if (!(await waitFor(async () => { const p = await panel(); return p.Sheet.Open && p.Sheet.Tab === tab; }, 5000))) throw new Error(`the ${tab} readout: sheet ${JSON.stringify((await panel()).Sheet.Tab)}`);
    await tap(TW - 52, 22);
    if (!(await waitFor(async () => !(await panel()).Sheet.Open, 5000))) throw new Error(`Close left the ${tab} sheet open`);
  }
  // TIME: the time/div row's +, the Zoom row's 5x
  await tap(4 + nch * 104 + 60, 20);
  await waitFor(async () => (await panel()).Sheet.Tab === "time", 5000);
  const t0 = (await rowOf("time.tdiv")).Value;
  const rowsX = TW - 760 + 120 + 8;
  await tap(TW - 8 - 124 - 50 + 22, await rowY("time", "time.tdiv"));
  const t1 = await waitFor(async () => { const v = (await rowOf("time.tdiv")).Value; return v !== t0 ? v : null; }, 5000);
  if (!t1) throw new Error(`the time/div row's + left ${t0}`);
  await tap(rowsX + 190 + 2 * 50 + 22, await rowY("time", "time.zoom"));
  if (!(await waitFor(async () => (await zoomNow()) === 5, 5000))) throw new Error(`the Zoom row's 5x: zoom ${await zoomNow()}`);
  await tap(100, 300); // beside the panel
  if (!(await waitFor(async () => !(await panel()).Sheet.Open, 5000))) throw new Error("a tap beside the sheet left it open");
  return { tdiv: [t0, t1] };
});

// The gestures act on the view of the record only: a box zooms into it,
// two fingers pan, a pinch zooms, a double tap resets; no setting moves.
add("touch-gestures-view-only", async () => {
  if (!(await touchFirst())) return { skip: "no touch-first screen" };
  await defaultSetup();
  await signals({ C1: "sine,1e3,2,0" });
  await sleep(1000);
  const rows0 = await settingRows();
  const mid = Math.round((40 + TH - 56) / 2);
  const log = [];
  await dragOn(300, mid - 80, 420, mid + 80);
  const z1 = await waitFor(async () => { const z = await zoomNow(); return z >= 5 ? z : null; }, 5000);
  log.push({ box: z1 });
  if (!z1) throw new Error(`a box a ninth of the screen wide: zoom ${await zoomNow()}`);
  const p0 = (await rowOf("time.zoom_pos")).Value;
  await dragOn(400, mid, 600, mid, "Shift");
  const p1 = await waitFor(async () => { const p = (await rowOf("time.zoom_pos")).Value; return p !== p0 ? p : null; }, 5000);
  log.push({ pan: [p0, p1] });
  if (p1 === null) throw new Error(`two fingers right left the pan at ${p0}`);
  const [px, py] = await screenAt(500, mid);
  await bench.mouse.dblclick(px, py);
  if (!(await waitFor(async () => (await zoomNow()) === 1, 5000))) throw new Error(`a double tap left zoom ${await zoomNow()}`);
  await pinchAt(500, mid, 1);
  await pinchAt(500, mid, 1);
  const z2 = await waitFor(async () => { const z = await zoomNow(); return z > 1 ? z : null; }, 5000);
  log.push({ pinch: z2 });
  if (!z2) throw new Error("two pinches out left the view unzoomed");
  for (let k = 0; k < 4 && (await zoomNow()) > 1; k++) await pinchAt(500, mid, -1);
  if ((await zoomNow()) !== 1) throw new Error(`pinches in left zoom ${await zoomNow()}`);
  const rows1 = await settingRows();
  if (rows1 !== rows0) throw new Error(`a gesture changed a setting:\n${rows0}\n${rows1}`);
  return { log };
});

// The bounded fuzz: random panel keys and knob turns; the scope keeps
// publishing and no page throws.
add("fuzz-panel", async () => {
  const st0 = await simState();
  const keys = (st0.keys || []).filter((k) => !/defaultsetup|saverecall|print/.test(k));
  const knobs = st0.knobs || [];
  let seed = Number(process.env.SEED || 1);
  // TRLC-LINKS: REQ-DSO-298
  const rnd = () => ((seed = (seed * 1103515245 + 12345) % 2147483648) / 2147483648);
  const iters = Number(process.env.FUZZ_ITERS || 40);
  const done = [];
  for (let i = 0; i < iters; i++) {
    if (rnd() < 0.6) { const k = keys[Math.floor(rnd() * keys.length)]; done.push(k); await key(k); }
    else { const k = knobs[Math.floor(rnd() * knobs.length)]; const s = rnd() < 0.5 ? -1 : 1; done.push(`${k}${s > 0 ? "+" : "-"}`); await knob(k, s); await sleep(150); }
  }
  await idle(60000);
  const a = (await status()).seq;
  await sleep(3000);
  const st = await status();
  if (st.running && st.seq === a && st.run_state !== "Arm" && st.run_state !== "Ready") throw new Error(`no frame in 3 s after ${done.join(" ")}`);
  return { actions: done.length, runState: st.run_state };
});

// ---------------------------------------------------------------- run
nch = (await simState()).channels || 2;
const results = [];
for (const c of cases) {
  if (only && !only.test(c.id)) continue;
  const t = Date.now();
  const nerr = errors.length;
  let r;
  try {
    const detail = await c.fn();
    r = { case: c.id, ok: errors.length === nerr, ms: Date.now() - t, detail, errors: errors.slice(nerr) };
  } catch (e) {
    r = { case: c.id, ok: false, ms: Date.now() - t, err: String(e.message || e), errors: errors.slice(nerr) };
    if (shots) {
      fs.mkdirSync(shots, { recursive: true });
      await ui.screenshot({ path: path.join(shots, `${c.id}-web.png`) }).catch(() => {});
      await bench.screenshot({ path: path.join(shots, `${c.id}-bench.png`), fullPage: true }).catch(() => {});
    }
  }
  results.push(r);
  console.log(JSON.stringify(r));
}
await browser.close();
const pass = results.filter((r) => r.ok).length;
console.log(JSON.stringify({ summary: { mode: native ? "native" : "wasm", loadMs, pass, total: results.length } }));
process.exit(pass === results.length ? 0 : 1);
