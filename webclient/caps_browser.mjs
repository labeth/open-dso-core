// ENGMODEL-OWNER-UNIT: FU-WEB-APP
// Drives the real page against a scope and prints what it built and asked
// for, as JSON, for webserver.TestCapsBrowser to compare: the requests it
// made, the channel lists, the measurement and cursor tables and the frame
// it draws. Prints SKIP when no browser is available.
import { findPlaywright, pageObject } from "./scope_po.mjs";

const url = process.argv[2];
const pw = findPlaywright();
if (!pw) { console.log("SKIP playwright not found"); process.exit(0); }
const { chromium } = (await import(pw)).default;
let browser;
try {
  browser = await chromium.launch({ headless: true, args: ["--no-sandbox", "--use-gl=egl", "--enable-gpu", "--ignore-gpu-blocklist"] });
} catch (e) { console.log("SKIP " + e.message.split("\n")[0]); process.exit(0); }
const page = await browser.newPage({ viewport: { width: 1400, height: 900 } });
const errors = [], requests = new Set();
page.on("pageerror", (e) => errors.push(e.message));
page.on("request", (r) => {
  const u = new URL(r.url());
  if (!u.pathname.startsWith("/api/")) return;
  const q = [...u.searchParams.entries()].map(([k, v]) => k + "=" + (["since", "epoch"].includes(k) ? "N" : v)).sort().join("&");
  requests.add(r.method() + " " + u.pathname + (q ? "?" + q : "") + (r.postData() ? " " + r.postData() : ""));
});
// a scope of more than two channels shows a reference of each of its
// channels (the shared server's references hold two today, so the page's
// reading of more is checked with one served here)
const capsDoc = await fetch(url + "/api/caps").then((r) => (r.ok ? r.json() : null)).catch(() => null);
const wide = !!(capsDoc && capsDoc.channels && capsDoc.channels.length > 2);
if (wide) {
  const cols = (k) => Array.from({ length: 8 }, (_, i) => 100 + 10 * k + i);
  const ref = { has: true, show: true, gen: 7, at: 0, cols: 8, c1: cols(1), c2: cols(2), c3: cols(3), c4: cols(4),
    vdiv: [0.5, 0.5, 0.5, 0.5], off: [0, 0, 0, 0], probe: [1, 1, 1, 1], tdiv_s: 1e-6, pos_frac: 0.5 };
  await page.route("**/api/refs", (route) => route.request().method() === "GET"
    ? route.fulfill({ contentType: "application/json", body: JSON.stringify({ ok: true, refs: [ref, { has: false }] }) })
    : route.continue());
}
// SOUND's stream names the channel each of its two lanes carries in its
// scales' chans; the shared server's sound wire has no such field yet, so
// for the four-channel scope the lanes (the tuned channel and C1) are
// named here, in the reply's JSON header
let soundLanes = null;
if (wide) {
  await page.route(/\/api\/sound(\/take)?\.bin/, async (route) => {
    const r = await route.fetch(), b = Buffer.from(await r.body());
    if (!soundLanes || b.length < 8) return route.fulfill({ response: r, body: b });
    const hl = b.readUInt32LE(4), h = JSON.parse(b.subarray(8, 8 + hl).toString());
    for (const sc of h.scales || []) sc.chans = soundLanes;
    const hj = Buffer.from(JSON.stringify(h)), head = Buffer.alloc(8);
    b.copy(head, 0, 0, 4); head.writeUInt32LE(hj.length, 4);
    return route.fulfill({ response: r, body: Buffer.concat([head, hj, b.subarray(8 + hl)]) });
  });
}
await page.goto(url, { waitUntil: "domcontentloaded" });
await page.waitForFunction(() => typeof frame !== "undefined" && frame && frame.c1 && document.getElementById("tdiv").options.length > 0, null, { timeout: 15000 });
await page.waitForTimeout(1500); // the caps a status names are read and applied
const po = pageObject(page);
await po.click("tCursors");
const n = await page.evaluate(() => chanNums().length);
// the last channel's V/div steps and its display toggles
await po.click("vdiv" + n + "Up");
await po.click("tC" + n);
await page.waitForTimeout(600);
const out = await page.evaluate(() => {
  const f = frame, arr = (a) => a ? { type: a.constructor.name, n: a.length, head: Array.from(a.slice(0, 40)) } : null;
  const opts = (id) => [...document.querySelectorAll("#" + id + " > option, #" + id + " > button")].map((o) => o.textContent.trim() + "=" + (o.value || o.dataset.value));
  return {
    capsServed, chans: chanNums(), colors: chanNums().map(chanColor), wire: f.wire || 1,
    frame: Object.fromEntries(chanNums().map((k) => ["c" + k, arr(f["c" + k])])),
    vpc: chanNums().map((k) => f["vpc" + k]),
    shown: chanNums().map((k) => view["c" + k]),
    toggles: [...document.querySelectorAll('[id^="tC"]')].map((e) => e.id).filter((id) => /^tC\d+$/.test(id)),
    source: opts("source"), decScl: opts("decScl"), measCh: opts("measCh"), zmCh: opts("zmCh"), ejCh: opts("ejCh"),
    ttypeDisabled: [...document.querySelectorAll("#ttype > button")].map((b) => b.disabled),
    quick: document.getElementById("quickBody").innerHTML.replace(/>[^<]*</g, "><"),
    meas: document.getElementById("measBody").innerHTML.replace(/>[^<]*</g, "><"),
    cursorRows: [...document.querySelectorAll("#curBody th")].map((th) => th.textContent),
    vgCells: document.querySelector(".vg").children.length,
    brand: document.querySelector(".brand > span").textContent,
  };
});
// ---- the parts that follow the caps' channels, on the last channel ----
const last = n;
await po.click("tC" + last); // shown again
const vert = await page.evaluate(() => {
  const vg = document.querySelector("#p-vert .vg");
  return { over: [...vg.children].filter((c) => c.scrollWidth > c.clientWidth + 1).length + (vg.scrollWidth > vg.clientWidth + 1 ? 1 : 0) };
});
if (process.env.CAPS_SHOT) await page.locator("#p-vert").screenshot({ path: process.env.CAPS_SHOT + "-" + last + ".png" });
// the trigger level, in volts
await page.evaluate(() => { const l = document.getElementById("lvl"); l.value = "0.25"; l.dispatchEvent(new Event("change")); });
// a mask built from the raw feed on the last channel
await page.evaluate((k) => {
  document.getElementById("zmN").value = "4";
  document.querySelector(`#zmCh > button[data-value="${k - 1}"]`).click();
  document.getElementById("zmBuild").click();
}, last);
await page.waitForFunction(() => /mask built|failed/.test(document.getElementById("zmStats").textContent), null, { timeout: 15000 });
// its coupling and bandwidth limit
await page.evaluate((k) => {
  document.querySelector(`#cpl${k} > button[data-value="1"]`).click();
  document.querySelector(`#bwl${k} > button[data-value="1"]`).click();
}, last);
// its spectrum's peaks, math on it, X-Y of it
const areas = await page.evaluate((k) => {
  document.querySelector(`#pkCh > button[data-value="${k}"]`).click();
  computePeaksCh(k); updateFFTListCh(k);
  const mf = document.getElementById("mathFn");
  mf.value = k > 2 ? "c3-c4" : "c1-c2"; mf.dispatchEvent(new Event("change"));
  const m = computeMath();
  document.getElementById("mXY").click();
  if (document.getElementById("xyX")) {
    for (const [id, v] of [["xyX", "3"], ["xyY", "4"]]) { const e = document.getElementById(id); e.value = v; e.dispatchEvent(new Event("change")); }
  }
  redraw();
  const xyOpts = document.getElementById("xyX") ? [...document.getElementById("xyX").options].map((o) => o.textContent) : null;
  document.getElementById("mYT").click();
  // the exports, as the files they would save
  const got = {};
  window.exportFile = (name, type, data) => { got[name.split(".").pop()] = data; };
  document.getElementById("eCSV").click(); document.getElementById("eWAV").click(); document.getElementById("eVCD").click();
  const csv = got.csv.split("\n"), w = got.wav;
  const row = csv[2].split(",");
  return {
    peaks: fftCh[k].peaks.length > 0, pkSub: document.getElementById("pkSub").textContent,
    spgCh: [...document.getElementById("spgCh").options].map((o) => o.textContent),
    mathOpts: [...mf.options].map((o) => o.value), mathFn, mathN: m ? m.length : 0,
    xyOpts, xyPair: [...xyPair],
    refChans: refs.A ? refChans(refs.A) : [], refInfo: document.getElementById("refInfoA").textContent.replace(/ · [^·]*$/, ""),
    csvHead: csv[1], csvVolts: row.slice(1).every((v) => v === "" || Number.isFinite(+v)) && row.length,
    wavCh: w ? w[22] | (w[23] << 8) : 0, vcdVars: (got.vcd.match(/\$var real/g) || []).length,
  };
}, last);
// All settings lists every channel
await po.click("allBtn");
const allGroups = await page.evaluate(() => [...document.querySelectorAll("#allList .ag")].map((g) => g.dataset.group).filter((g) => /^Channel/.test(g)));
const allRows = await page.evaluate((k) => ["tC", "vdiv", "cpl", "inv"].every((p) => !!document.querySelector(`#allList .ar[data-entry="${p}${k}"]`)), last);
await po.click("allClose");
// SOUND tuned to the last channel: the lanes it streams name its choices
await po.ws("sound");
if (wide) soundLanes = [last - 1, 0];
await page.evaluate((k) => document.querySelector(`#sndCh > button[data-value="${k - 1}"]`).click(), last);
await page.waitForTimeout(1500);
const sound = await page.evaluate(() => ({
  src: [...document.querySelectorAll('#sndSrc > button[data-value^="c"]')].map((b) => b.textContent),
  meters: [1, 2].map((k) => document.querySelector(`#sndMeter${k} .snd-mh > b`).textContent),
  gate: [...document.getElementById("sndGate").options].map((o) => o.textContent),
}));
await po.ws("scope");
Object.assign(out, { vertOver: vert.over, areas, allGroups, allRows, sound });
// a caps document of fewer channels takes the last one's controls away
if (wide) {
  const fewer = Object.assign({}, capsDoc, { caps_gen: 99, channels: capsDoc.channels.slice(0, 3) });
  fewer.trigger = Object.assign({}, capsDoc.trigger, { sources: ["C1", "C2", "C3"] });
  await page.route("**/api/caps", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(fewer) }));
  await page.route("**/api/status", async (route) => {
    const r = await route.fetch(), j = await r.json();
    j.caps_gen = 99;
    route.fulfill({ response: r, body: JSON.stringify(j) });
  });
  await page.waitForFunction(() => chanNums().length === 3, null, { timeout: 8000 });
  await page.waitForTimeout(300);
  await po.click("allBtn");
  out.fewer = await page.evaluate(() => ({
    toggles: [...document.querySelectorAll('[id^="tC"]')].map((e) => e.id).filter((id) => /^tC\d+$/.test(id)),
    vgCells: document.querySelector(".vg").children.length,
    allGroups: [...document.querySelectorAll("#allList .ag")].map((g) => g.dataset.group).filter((g) => /^Channel/.test(g)),
    mathOpts: document.getElementById("mathFn").options.length,
    gate: document.getElementById("sndGate").options.length,
    source: [...document.querySelectorAll("#source > button")].map((b) => b.textContent),
    keys: KEYMAP.filter((k) => /^Show \/ hide channel/.test(k.desc)).map((k) => k.key),
    stale: MIRRORS.filter((r) => !r.src.isConnected).length,
  }));
}
out.errors = errors;
out.requests = [...requests].sort();
console.log(JSON.stringify(out));
await browser.close();
