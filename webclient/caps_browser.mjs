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
out.errors = errors;
out.requests = [...requests].sort();
console.log(JSON.stringify(out));
await browser.close();
