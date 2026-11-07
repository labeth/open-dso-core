// The demo's service worker: the scope's web API and its web page are answered
// by the demo running as WebAssembly in the bench page's worker, so the web UI
// runs unchanged. A request for /api/… (the web UI asks by absolute path) or
// for anything under ui/ (the web UI's own page and scripts) goes to the
// engine over a MessagePort the bench page hands over; everything else (the
// bench page, the module, the scope's data) is the static site.
'use strict';

const base = new URL(self.registration.scope).pathname; // "/" or "/open-dso/cml/"
let port = null;
let portWaiters = [];
let nextId = 1;
const pending = new Map();

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

self.addEventListener('message', (e) => {
  if (e.data?.type !== 'port' || !e.ports[0]) return;
  if (port) port.onmessage = null;
  port = e.ports[0];
  port.onmessage = (m) => {
    const r = m.data;
    const p = pending.get(r.id);
    if (!p) return;
    pending.delete(r.id);
    p(r);
  };
  for (const w of portWaiters) w(port);
  portWaiters = [];
});

// enginePort is the port to the engine, asked of the bench page when there is
// none yet (a service worker the browser restarted forgets it).
async function enginePort() {
  if (port) return port;
  const got = new Promise((res) => portWaiters.push(res));
  for (const c of await self.clients.matchAll({ type: 'window', includeUncontrolled: true })) {
    c.postMessage({ type: 'need-port' });
  }
  const timeout = new Promise((res) => setTimeout(() => res(null), 15000));
  return Promise.race([got, timeout]);
}

// target is the engine's path for a request, or null for the static site.
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

async function forward(req, path) {
  // The engine's replies are never compressed: a Response built here is not
  // decoded again, so the request asks for none (accept-encoding is dropped).
  const p = await enginePort();
  if (!p) {
    return new Response('The demo engine is not running: open the demo page first.', { status: 503, headers: { 'Content-Type': 'text/plain' } });
  }
  const body = req.method === 'GET' || req.method === 'HEAD' ? null : new Uint8Array(await req.arrayBuffer());
  const id = nextId++;
  const reply = new Promise((res) => pending.set(id, res));
  p.postMessage({ id, method: req.method, url: path, headers: [...req.headers].filter(([k]) => k !== 'accept-encoding'), body }, body ? [body.buffer] : []);
  const r = await reply;
  if (r.error) return new Response(r.error, { status: 502, headers: { 'Content-Type': 'text/plain' } });
  const h = new Headers();
  for (const [k, v] of r.headers) h.append(k, v);
  const nobody = req.method === 'HEAD' || r.status === 204 || r.status === 304;
  return new Response(nobody ? null : r.body, { status: r.status, headers: h });
}
