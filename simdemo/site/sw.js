// The demo's service worker: the scope's web API and its web page are answered
// by the demo running as WebAssembly in the bench page's worker, so the web UI
// runs unchanged. A request for /api/… (the web UI asks by absolute path) or
// for anything under ui/ (the web UI's own page and scripts) goes to the
// engine through the bench page, which owns it; everything else (the
// bench page, the module, the scope's data) is the static site.
'use strict';

const base = new URL(self.registration.scope).pathname; // "/" or "/open-dso/cml/"
let nextId = 1;
const pending = new Map();

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

// The bench page answers each request from its engine and posts the answer
// back here. Messages, not a port handed over: a port passed into a service
// worker does not carry the engine's answers back in every browser.
// TRLC-LINKS: REQ-DSO-298
self.addEventListener('message', (e) => {
  const r = e.data;
  if (r?.type !== 'answer') return;
  const p = pending.get(r.id);
  if (!p) return;
  pending.delete(r.id);
  p(r);
});

// benchPage is the window that runs the engine: the bench page, not the web
// UI it frames (whose pages are under ui/).
// TRLC-LINKS: REQ-DSO-298
async function benchPage() {
  for (let i = 0; i < 60; i++) {
    const all = await self.clients.matchAll({ type: 'window', includeUncontrolled: true });
    const page = all.find((c) => {
      const p = new URL(c.url).pathname;
      return p === base || p === base + 'index.html';
    });
    if (page) return page;
    await new Promise((res) => setTimeout(res, 250));
  }
  return null;
}

// target is the engine's path for a request, or null for the static site.
// TRLC-LINKS: REQ-DSO-298
function target(url) {
  if (url.origin !== self.location.origin) return null;
  const p = url.pathname;
  if (p.startsWith('/api/')) return p + url.search;
  if (p.startsWith(base + 'ui/')) return '/' + p.slice(base.length + 3) + url.search;
  if (p === base + 'ui') return '/' + url.search;
  return null;
}

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  const path = target(url);
  if (path === null) return;
  e.respondWith(forward(e.request, path));
});

// TRLC-LINKS: REQ-DSO-298
async function forward(req, path) {
  // The engine's replies are never compressed: a Response built here is not
  // decoded again, so the request asks for none (accept-encoding is dropped).
  const page = await benchPage();
  if (!page) {
    return new Response('The demo engine is not running: open the demo page first.', { status: 503, headers: { 'Content-Type': 'text/plain' } });
  }
  const body = req.method === 'GET' || req.method === 'HEAD' ? null : new Uint8Array(await req.arrayBuffer());
  const id = nextId++;
  const reply = new Promise((res) => pending.set(id, res));
  page.postMessage({ type: 'request', id, method: req.method, url: path, headers: [...req.headers].filter(([k]) => k !== 'accept-encoding'), body }, body ? [body.buffer] : []);
  const r = await reply;
  if (r.error) return new Response(r.error, { status: 502, headers: { 'Content-Type': 'text/plain' } });
  const h = new Headers();
  for (const [k, v] of r.headers) h.append(k, v);
  const nobody = req.method === 'HEAD' || r.status === 204 || r.status === 304;
  return new Response(nobody ? null : r.body, { status: r.status, headers: h });
}
