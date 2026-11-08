// The demo's engine worker: runs the demo's WebAssembly module (cmd/dsodemo-wasm)
// off the page's thread, answers the service worker's requests through the
// port the bench page hands over, and posts the board's pictures, lamps and
// store changes to the page.
'use strict';

importScripts('wasm_exec.js');

let ready = false;
const ports = [];

self.dsoHost = {
  // TRLC-LINKS: REQ-DSO-298
  present(rgba, w, h) { self.postMessage({ type: 'present', w, h, rgba }, [rgba.buffer]); },
  // TRLC-LINKS: REQ-DSO-298
  lamps(on) { self.postMessage({ type: 'lamps', on }); },
  // TRLC-LINKS: REQ-DSO-298
  persist(store, key, b64) { self.postMessage({ type: 'persist', store, key, b64 }); },
  // TRLC-LINKS: REQ-DSO-298
  log(line) { self.postMessage({ type: 'log', line }); },
  // TRLC-LINKS: REQ-DSO-298
  ready() {
    ready = true;
    for (const p of ports.splice(0)) servePort(p);
    self.postMessage({ type: 'ready' });
  },
  // TRLC-LINKS: REQ-DSO-298
  failed(err) { self.postMessage({ type: 'failed', err }); },
};

// servePort answers every request that arrives on p.
// TRLC-LINKS: REQ-DSO-298
function servePort(p) {
  // TRLC-LINKS: REQ-DSO-298
  p.onmessage = async (e) => {
    const r = e.data;
    try {
      const res = await self.dsoServe({ method: r.method, url: r.url, headers: r.headers, body: r.body });
      p.postMessage({ id: r.id, status: res.status, headers: res.headers, body: res.body }, [res.body.buffer]);
    } catch (err) {
      p.postMessage({ id: r.id, error: String(err) });
    }
  };
}

// TRLC-LINKS: REQ-DSO-298
self.onmessage = async (e) => {
  const m = e.data;
  if (m.type === 'port') {
    if (ready) servePort(e.ports[0]);
    else ports.push(e.ports[0]);
    return;
  }
  if (m.type !== 'start') return;
  self.dsoDemoConfig = { profile: m.profile, demo: m.demo, stored: m.stored };
  const go = new Go();
  const t0 = performance.now();
  try {
    const res = await WebAssembly.instantiateStreaming(fetch(m.wasm), go.importObject);
    self.postMessage({ type: 'loaded', ms: performance.now() - t0 });
    go.run(res.instance);
  } catch (err) {
    self.postMessage({ type: 'failed', err: String(err) });
  }
};
