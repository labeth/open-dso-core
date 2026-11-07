// ENGMODEL-OWNER-UNIT: FU-DSO-WEBCLIENT
// Loads the real page against any scope and prints, as one JSON line, what
// it made of it: the channels it built from caps, the frame wire it read,
// each drawn channel's span in display units and volts per unit, the page
// errors and the API replies that failed, and what a V/div step on C1 did
// to the scope's status. A scope's own harness (a simulator's, a bench's)
// judges the numbers against what it injected. Prints SKIP when no browser
// is available.
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
const errors = [], failed = [];
page.on("pageerror", (e) => errors.push(e.message));
page.on("response", (r) => {
  const u = new URL(r.url());
  // a part the scope lacks answers 404 by design; anything else is a failure
  if (u.pathname.startsWith("/api/") && r.status() >= 400 && r.status() !== 404) failed.push(r.status() + " " + u.pathname);
});
// the scope's status now, as the page reads it
// TRLC-LINKS: REQ-DSO-164
const status = () => fetch(url + "/api/status").then((r) => r.json()).catch(() => ({}));
await page.goto(url, { waitUntil: "domcontentloaded" });
await page.waitForFunction(() => typeof frame !== "undefined" && frame && frame.c1 && document.getElementById("tdiv").options.length > 0, null, { timeout: 20000 });
await page.waitForTimeout(1500); // the caps a status names are read and applied
const before = (await status()).vdiv1;
await pageObject(page).click("vdiv1Up");
let after = before;
for (let i = 0; i < 40 && after === before; i++) {
  await page.waitForTimeout(100);
  after = (await status()).vdiv1;
}
const out = await page.evaluate(() => {
  // TRLC-LINKS: REQ-DSO-1004
  const span = (a) => {
    if (!a || !a.length) return null;
    let lo = Infinity, hi = -Infinity;
    for (const v of a) { if (v < lo) lo = v; if (v > hi) hi = v; }
    return { n: a.length, type: a.constructor.name, lo, hi };
  };
  const drawn = Object.fromEntries(chanNums().filter((k) => frame["c" + k]).map((k) => ["c" + k, { ...span(frame["c" + k]), vpc: frame["vpc" + k], off: frame["off" + k + "_v"] }]));
  return { capsServed, chans: chanNums(), wire: frame.wire || 1, drawn };
});
console.log(JSON.stringify({ ...out, vdiv1: [before, after], errors, failed }));
await browser.close();
