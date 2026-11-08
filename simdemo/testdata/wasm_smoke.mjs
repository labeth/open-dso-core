// The browser demo's smoke run: loads a built site in headless Chromium, waits
// for the firmware's first picture and the web UI's frames, presses RUN/STOP on
// the bench's panel and sees the web UI stop and run again, and reports the
// load time and frame rates as one JSON line.
//
//   node wasm_smoke.mjs URL
import path from "node:path";
import { findPlaywright } from "../../webclient/scope_po.mjs";

const url = process.argv[2];
const pw = findPlaywright();
if (!url || !pw) { console.log(JSON.stringify({ skip: !pw ? "playwright not installed" : "no URL" })); process.exit(0); }
if (!process.env.PLAYWRIGHT_BROWSERS_PATH && process.env.HOME)
  process.env.PLAYWRIGHT_BROWSERS_PATH = path.join(process.env.HOME, ".cache/ms-playwright");
const { chromium } = (await import(pw)).default;
const browser = await chromium.launch({ headless: true, args: ["--no-sandbox", "--use-gl=egl"] });
const out = { errors: [] };
try {
  const page = await browser.newPage({ viewport: { width: 1500, height: 1100 } });
  page.on("pageerror", (e) => out.errors.push(e.message));
  const t0 = Date.now();
  await page.goto(url, { waitUntil: "load" });
  await page.waitForFunction(() => window.demoStats && window.demoStats.firstFrameMs > 0, null, { timeout: 60000 });
  out.firstPictureMs = Date.now() - t0;
  out.moduleMs = await page.evaluate(() => window.demoStats.loadMs);
  // TRLC-LINKS: REQ-DSO-298
  const web = async () => {
    const f = page.frames().find((x) => x.url().includes("/ui/"));
    return f ? f.evaluate(() => (typeof frame !== "undefined" && frame ? frame.seq : 0)).catch(() => 0) : 0;
  };
  for (let i = 0; i < 120 && (await web()) === 0; i++) await page.waitForTimeout(250);
  out.webUpMs = Date.now() - t0;
  // rates over five seconds: the bench screen's pictures and the web UI's frames
  const f0 = await page.evaluate(() => window.demoStats.pictures), w0 = await web(), s0 = Date.now();
  await page.waitForTimeout(5000);
  const dt = (Date.now() - s0) / 1000;
  out.screenFps = (await page.evaluate(() => window.demoStats.pictures) - f0) / dt;
  out.webFps = ((await web()) - w0) / dt;
  // RUN/STOP on the bench's panel stops the acquisition the web UI shows, and runs it again
  // asked from the page: its service worker answers it from the engine
  // TRLC-LINKS: REQ-DSO-298
  const running = () => page.evaluate(async () => (await (await fetch("/api/status")).json()).running);
  await page.click('button[data-key="run"]');
  for (let i = 0; i < 40 && (await running()); i++) await page.waitForTimeout(100);
  out.stopped = !(await running());
  await page.click('button[data-key="run"]');
  for (let i = 0; i < 40 && !(await running()); i++) await page.waitForTimeout(100);
  out.runAgain = await running();
  await page.screenshot({ path: process.env.SHOT || "/dev/null", fullPage: true }).catch(() => {});
} catch (e) {
  out.errors.push(String(e.message || e));
} finally {
  await browser.close();
}
console.log(JSON.stringify(out));
