// The WebAssembly demo as an HTTP target: loads a built demo site in
// headless Chromium, waits for its engine's first picture, then serves the
// scope's web API on a local port by asking the page to fetch each request
// (the page's service worker answers it from the engine). The session
// runner then speaks to the browser demo as to any scope.
//
//   node wasmbridge.mjs SITE_URL [PORT]
//
// Prints "READY http://127.0.0.1:PORT" once the demo runs, "SKIP: …" and
// exits 0 when Playwright is not installed.
import http from "node:http";
import path from "node:path";
import { findPlaywright } from "../webclient/scope_po.mjs";

const site = process.argv[2];
const port = Number(process.argv[3] || 0);
const pw = findPlaywright();
if (!site) { console.error("usage: node wasmbridge.mjs SITE_URL [PORT]"); process.exit(2); }
if (!pw) { console.log("SKIP: playwright not installed"); process.exit(0); }
if (!process.env.PLAYWRIGHT_BROWSERS_PATH && process.env.HOME)
  process.env.PLAYWRIGHT_BROWSERS_PATH = path.join(process.env.HOME, ".cache/ms-playwright");
const { chromium } = (await import(pw)).default;
const browser = await chromium.launch({ headless: true, args: ["--no-sandbox", "--use-gl=egl"] });
const page = await browser.newPage({ viewport: { width: 1500, height: 1100 } });
page.on("pageerror", (e) => console.error("pageerror:", e.message));
await page.goto(site, { waitUntil: "load" });
await page.waitForFunction(() => window.demoStats && window.demoStats.firstFrameMs > 0, null, { timeout: 120000 });

// relay runs one request in the page and returns its status, type and body.
// TRLC-LINKS: REQ-DSO-295
async function relay(method, url, body) {
  return page.evaluate(async ({ method, url, body }) => {
    const init = { method, cache: "no-store" };
    if (body !== null) { init.body = body; init.headers = { "Content-Type": "application/json" }; }
    const r = await fetch(url, init);
    const buf = new Uint8Array(await r.arrayBuffer());
    let s = "";
    for (let i = 0; i < buf.length; i += 0x8000) s += String.fromCharCode.apply(null, buf.subarray(i, i + 0x8000));
    return { status: r.status, type: r.headers.get("Content-Type") || "", body: btoa(s) };
  }, { method, url, body });
}

const server = http.createServer(async (req, res) => {
  const chunks = [];
  for await (const c of req) chunks.push(c);
  const body = chunks.length ? Buffer.concat(chunks).toString("utf8") : null;
  try {
    const r = await relay(req.method, req.url, req.method === "GET" || req.method === "HEAD" ? null : body);
    res.writeHead(r.status, { "Content-Type": r.type, "Cache-Control": "no-store" });
    res.end(Buffer.from(r.body, "base64"));
  } catch (e) {
    res.writeHead(502, { "Content-Type": "text/plain" });
    res.end("bridge: " + (e.message || e));
  }
});
server.listen(port, "127.0.0.1", () => console.log("READY http://127.0.0.1:" + server.address().port));
// TRLC-LINKS: REQ-DSO-295
const stop = async () => { server.close(); await browser.close().catch(() => {}); process.exit(0); };
process.on("SIGTERM", stop);
process.on("SIGINT", stop);
process.stdin.on("end", stop);
process.stdin.resume();
