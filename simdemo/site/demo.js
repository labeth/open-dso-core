// The demo's bench page: the scope's front panel and screen, the signal
// generator on its inputs and the scope's own web UI, around the firmware.
// The firmware runs either in this tab (mode "wasm": a worker runs the
// WebAssembly module and a service worker answers the web UI's requests from
// it) or in a local demo server (mode "native"). The scope's data comes from
// scope/demo.json (its panel layout) and scope/profile.yaml.
'use strict';

const $ = (s, el = document) => el.querySelector(s);
const h = (tag, attrs = {}, ...kids) => {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') e.className = v;
    else if (k.startsWith('on')) e.addEventListener(k.slice(2), v);
    else if (v !== false && v != null) e.setAttribute(k, v === true ? '' : v);
  }
  for (const k of kids.flat()) if (k != null) e.append(k);
  return e;
};

async function api(path, body) {
  const r = await fetch(path, body === undefined ? {} : {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
  });
  const j = await r.json().catch(() => ({ ok: false, err: r.statusText }));
  if (!r.ok && j.ok !== false) j.ok = false;
  return j;
}

let site = { mode: 'wasm', ui: 'ui/' };
let demo = {};
let channels = 2;
const inputs = () => Array.from({ length: channels }, (_, i) => `C${i + 1}`);
const stats = { t0: performance.now(), loadMs: 0, frames: 0, pictures: 0, fpsAt: performance.now(), fps: 0, firstFrameMs: 0 };
window.demoStats = stats; // read by the demo's browser tests

// ---------------------------------------------------------------- stores
// The board's settings and setups, kept in this browser between visits.
const storePrefix = () => `dso-demo/${demo.name || 'scope'}/`;
function loadStored() {
  const out = {};
  const pre = storePrefix();
  for (let i = 0; i < localStorage.length; i++) {
    const k = localStorage.key(i);
    if (!k.startsWith(pre)) continue;
    const [store, ...rest] = k.slice(pre.length).split('/');
    (out[store] ||= {})[rest.join('/')] = localStorage.getItem(k);
  }
  return out;
}
function persist(store, key, b64) {
  try { localStorage.setItem(`${storePrefix()}${store}/${key}`, b64); } catch { /* full or blocked: the demo runs without */ }
}

// ---------------------------------------------------------------- the firmware
let worker = null;

async function startWasm() {
  if (!('serviceWorker' in navigator)) throw new Error('this browser has no service workers: the web UI cannot reach the firmware');
  const reg = await navigator.serviceWorker.register('sw.js');
  await navigator.serviceWorker.ready;
  if (!navigator.serviceWorker.controller) {
    await new Promise((res) => navigator.serviceWorker.addEventListener('controllerchange', res, { once: true }));
  }
  navigator.serviceWorker.addEventListener('message', (e) => { if (e.data?.type === 'need-port') handPort(); });
  const [profile, demoText] = await Promise.all([
    fetch('scope/profile.yaml').then((r) => r.text()),
    fetch('scope/demo.json').then((r) => r.text()),
  ]);
  worker = new Worker('engine.js');
  const ready = new Promise((res, rej) => {
    worker.onmessage = (e) => {
      const m = e.data;
      switch (m.type) {
        case 'present': drawScreen(m.rgba, m.w, m.h); break;
        case 'lamps': showLamps(m.on); break;
        case 'persist': persist(m.store, m.key, m.b64); break;
        case 'log': console.debug('[firmware]', m.line); break;
        case 'loaded': stats.loadMs = m.ms; break;
        case 'ready': res(); break;
        case 'failed': rej(new Error(m.err)); break;
      }
    };
  });
  worker.postMessage({ type: 'start', wasm: 'dsodemo.wasm', profile, demo: demoText, stored: loadStored() });
  await ready;
  handPort();
  return reg;
}

// handPort gives the service worker a fresh port to the engine.
function handPort() {
  const ch = new MessageChannel();
  worker.postMessage({ type: 'port' }, [ch.port1]);
  navigator.serviceWorker.controller?.postMessage({ type: 'port' }, [ch.port2]);
}

// The native server's screen: the next picture by long poll.
async function pollScreen() {
  let seq = 0;
  for (;;) {
    try {
      const r = await fetch(`/api/sim/screen.rgba?after=${seq}`);
      const n = +r.headers.get('X-Screen-Seq');
      const w = +r.headers.get('X-Screen-W'), ht = +r.headers.get('X-Screen-H');
      const buf = new Uint8ClampedArray(await r.arrayBuffer());
      if (n > seq && buf.length === w * ht * 4) drawScreen(buf, w, ht);
      seq = n;
    } catch { await new Promise((res) => setTimeout(res, 1000)); }
  }
}

// ---------------------------------------------------------------- screen
let screenUp = false;
function drawScreen(rgba, w, ht) {
  const c = $('#screen');
  if (c.width !== w || c.height !== ht) { c.width = w; c.height = ht; }
  c.getContext('2d').putImageData(new ImageData(rgba, w, ht), 0, 0);
  if (!screenUp) {
    screenUp = true;
    stats.firstFrameMs = performance.now() - stats.t0;
    c.classList.add('up');
    $('#screenmsg').hidden = true;
  }
  stats.frames++;
  stats.pictures++;
}

function wireScreen() {
  const c = $('#screen');
  if (!demo.touch) return;
  c.classList.add('touch');
  c.addEventListener('click', async (e) => {
    if (!screenUp) return;
    const r = c.getBoundingClientRect();
    const x = Math.round((e.clientX - r.left) / r.width * c.width);
    const y = Math.round((e.clientY - r.top) / r.height * c.height);
    const rip = h('div', { class: 'ripple' });
    rip.style.left = `${e.clientX - r.left}px`;
    rip.style.top = `${e.clientY - r.top}px`;
    c.parentElement.append(rip);
    setTimeout(() => rip.remove(), 500);
    const res = await api('/api/sim/input', { touch: { x, y } });
    if (!res.ok) flash(`touch: ${res.err}`, true);
  });
}

// ---------------------------------------------------------------- front panel
// The layout is the scope's (demo.json "panel"): items at grid cells, each a
// key, a knob or a section label, by the UI model's key and knob names. A knob
// or key whose name holds {ch} follows the channel key pressed last (a panel
// with one set of vertical knobs for all its channels).
const LAMP_COLOUR = {
  ch1: 'var(--c1)', ch2: 'var(--c2)', ch3: 'var(--c3)', ch4: 'var(--c4)', run: 'var(--run)', stop: 'var(--stop)',
  math: 'var(--math)', auto: 'var(--run)', normal: 'var(--trigger)', single: 'var(--trigger)',
};
const keyEls = [];
const knobEls = {};
let selCh = 1;
let msgTimer;
const named = (n) => n.replaceAll('{ch}', String(selCh));

function flash(text, err) {
  const m = $('#genmsg');
  m.textContent = text;
  m.classList.toggle('err', !!err);
  clearTimeout(msgTimer);
  msgTimer = setTimeout(() => { m.textContent = ''; }, err ? 8000 : 3000);
}

async function press(key, el) {
  el?.classList.add('down');
  const m = /^ch(\d)$/.exec(key);
  if (m) selCh = +m[1];
  const r = await api('/api/sim/input', { key: named(key) });
  el?.classList.remove('down');
  if (!r.ok) flash(`${key}: ${r.err}`, true);
}

function keyButton(it) {
  const label = it.label.replace(/- /, '-​');
  const b = h('button', { class: `key ${it.cls || ''} ${it.big ? 'big' : ''}`, title: it.title || it.label.replace(/- /, ''), 'data-key': it.key },
    h('span', { class: 'led' }), label);
  if (it.inert) {
    // a key of the panel this firmware gives no job yet
    b.disabled = true;
    b.title = `${b.title}: no function in this firmware`;
  } else b.addEventListener('click', () => press(it.key, b));
  b._lamps = it.lamps || [it.key];
  keyEls.push(b);
  return b;
}

function buildPanel(panel) {
  const ctl = $('#controls');
  ctl.style.setProperty('--cols', panel.columns || 7);
  for (const it of panel.softkeys || []) $('#softkeys').append(keyButton(it));
  for (const it of panel.items || []) {
    const place = (e) => {
      e.style.gridColumn = `${it.c} / span ${it.cs || 1}`;
      e.style.gridRow = `${it.r} / span ${it.rs || 1}`;
      return e;
    };
    if (it.sect) { ctl.append(place(h('div', { class: 'sect' }, it.sect))); continue; }
    if (it.key) { ctl.append(place(keyButton(it))); continue; }
    const k = h('div', { class: `knob ${it.big ? 'big' : ''}`, tabindex: '0', role: 'slider',
      'aria-label': `${it.label} knob: drag or scroll to turn, click to push`, 'data-knob': it.knob }, h('span', { class: 'ind' }));
    const cell = h('div', { class: 'knobcell' }, k, h('span', { class: 'knoblabel' }, it.label));
    knobEls[it.knob] = k;
    wireKnob(k, it);
    ctl.append(place(cell));
  }
}

// A knob: detents accumulate and go out as one turn at a time.
function wireKnob(el, it) {
  let angle = 0, pending = 0, busy = false;
  const turn = (n) => {
    pending += n;
    angle += n * 18;
    el.querySelector('.ind').style.transform = `rotate(${angle}deg)`;
    flush();
  };
  const flush = async () => {
    if (busy || pending === 0) return;
    busy = true;
    const n = Math.max(-100, Math.min(100, pending));
    pending -= n;
    const r = await api('/api/sim/input', { knob: named(it.knob), steps: n });
    if (!r.ok) flash(`${it.label}: ${r.err}`, true);
    busy = false;
    if (pending) flush();
  };
  const push = async () => {
    if (!it.push) return;
    el.classList.add('pushed');
    const r = await api('/api/sim/input', { key: named(it.push) });
    el.classList.remove('pushed');
    if (!r.ok) flash(`${it.label} push: ${r.err}`, true);
  };
  let y0 = null, acc = 0, moved = false;
  el.addEventListener('pointerdown', (e) => { y0 = e.clientY; acc = 0; moved = false; el.setPointerCapture(e.pointerId); el.focus(); });
  el.addEventListener('pointermove', (e) => {
    if (y0 === null) return;
    acc += y0 - e.clientY;
    y0 = e.clientY;
    while (Math.abs(acc) >= 12) {
      const d = Math.sign(acc);
      acc -= 12 * d;
      moved = true;
      turn(d);
    }
  });
  el.addEventListener('pointerup', () => { if (y0 !== null && !moved) push(); y0 = null; });
  el.addEventListener('pointercancel', () => { y0 = null; });
  el.addEventListener('wheel', (e) => { e.preventDefault(); turn(e.deltaY < 0 ? 1 : -1); }, { passive: false });
  el.addEventListener('keydown', (e) => {
    if (e.key === 'ArrowUp' || e.key === 'ArrowRight') { turn(1); e.preventDefault(); }
    if (e.key === 'ArrowDown' || e.key === 'ArrowLeft') { turn(-1); e.preventDefault(); }
    if (e.key === 'Enter' || e.key === ' ') { push(); e.preventDefault(); }
  });
}

let lampsNow = [];
function showLamps(on) {
  lampsNow = on || [];
  const lit = new Set(lampsNow);
  for (const b of keyEls) {
    const l = b._lamps.find((x) => lit.has(x));
    b.classList.toggle('lit', !!l);
    if (l) b.style.setProperty('--lamp', LAMP_COLOUR[l] || '#e6ecf2');
  }
  knobEls.adjust?.classList.toggle('lamp', lit.has('adjust'));
}

// ---------------------------------------------------------------- numbers
const SI = { p: 1e-12, n: 1e-9, u: 1e-6, 'µ': 1e-6, m: 1e-3, '': 1, k: 1e3, K: 1e3, M: 1e6, G: 1e9 };
function parseSI(s) {
  const m = String(s).trim().match(/^([-+]?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?)\s*([pnuµmkKMG]?)[a-zA-Z]*$/);
  if (!m) return NaN;
  return parseFloat(m[1]) * SI[m[2]];
}
function fmtSI(v) {
  if (!isFinite(v)) return '';
  if (v === 0) return '0';
  const a = Math.abs(v);
  for (const [p, f] of [['G', 1e9], ['M', 1e6], ['k', 1e3], ['', 1], ['m', 1e-3], ['u', 1e-6], ['n', 1e-9], ['p', 1e-12]]) {
    if (a >= f * 0.9999) return `${+(v / f).toPrecision(6)}${p}`;
  }
  return String(v);
}
const num = (v) => String(+(+v).toPrecision(9));

// ---------------------------------------------------------------- signal generator
const WAVES = ['sine', 'square', 'pulse', 'triangle', 'ramp', 'dc', 'noise'];
const PROTOS = {
  uart: { name: 'UART', roles: { tx: 'TX' }, def: { tx: 'C1' }, rate: '115200', data: 'Hello', fmt: 'text' },
  spi: { name: 'SPI', roles: { clk: 'CLK', mosi: 'MOSI', miso: 'MISO', cs: 'CS' }, def: { clk: 'C1', mosi: 'C2', cs: 'C3' }, rate: '1M', data: 'A5 3C', fmt: 'hex' },
  i2c: { name: 'I²C', roles: { scl: 'SCL', sda: 'SDA' }, def: { scl: 'C1', sda: 'C2' }, rate: '100k', data: '00 A5', fmt: 'hex' },
  can: { name: 'CAN', roles: { rx: 'RX (logic)', h: 'CAN-H', l: 'CAN-L' }, def: { rx: 'C1' }, rate: '500k', data: 'DE AD BE EF', fmt: 'hex' },
  lin: { name: 'LIN', roles: { tx: 'Bus' }, def: { tx: 'C1' }, rate: '19.2k', data: '01 02', fmt: 'hex' },
  manchester: { name: 'Manchester', roles: { tx: 'Line' }, def: { tx: 'C1' }, rate: '1M', data: 'A5 F0', fmt: 'hex' },
  prbs: { name: 'PRBS', roles: { data: 'Data', clk: 'Bit clock' }, def: { data: 'C1', clk: 'C2' }, rate: '2M', data: '', fmt: 'hex' },
};
const PRESETS = [
  { label: 'UART 115200 "Hello"', p: 'uart' },
  { label: 'SPI 1 MHz', p: 'spi' },
  { label: 'I²C EEPROM write', p: 'i2c' },
  { label: 'CAN 500 k on H/L', p: 'can', lines: { h: 'C1', l: 'C2' }, vpp: '2', offset: '2.5' },
  { label: 'LIN 19.2 k', p: 'lin', vpp: '12', offset: '6' },
  { label: 'Manchester 1 Mb/s', p: 'manchester' },
  { label: 'PRBS7 2 Mb/s + clock', p: 'prbs' },
];
let state = { signals: {} };
let tab = 'C1';
let formFor = null; // which tab the form shows

function parseSpec(spec) {
  const f = (spec || '').split(',');
  const o = { kind: f[0] || 'dc', freq: +f[1] || 0, vpp: +f[2] || 0, offset: +f[3] || 0, opts: {} };
  for (const kv of f.slice(4)) { const i = kv.indexOf('='); if (i > 0) o.opts[kv.slice(0, i)] = kv.slice(i + 1); }
  return o;
}
const protoOf = (kind) => kind.split('.')[0];
const isLine = (kind) => !!PROTOS[protoOf(kind)];

function buildTabs() {
  const t = $('#gentabs');
  t.innerHTML = '';
  for (const c of [...inputs(), 'Protocol']) {
    const b = h('button', { class: `sm ${c === tab ? 'on' : ''} cc${c[1] || ''}`, role: 'tab', 'aria-selected': c === tab ? 'true' : 'false' }, c);
    b.addEventListener('click', () => { tab = c; buildTabs(); buildForm(); });
    t.append(b);
  }
}

function row(label, ctl, hint) {
  return [h('label', {}, label), hint ? h('div', { class: 'pair' }, ctl, h('span', { class: 'hint' }, hint)) : ctl];
}
const inp = (id, value, attrs = {}) => h('input', { id, value, autocomplete: 'off', spellcheck: 'false', ...attrs });
function sel(id, options, value) {
  const s = h('select', { id });
  for (const o of options) {
    const [v, l] = Array.isArray(o) ? o : [o, o];
    s.append(h('option', { value: v, selected: String(v) === String(value) }, l));
  }
  return s;
}

function buildForm() {
  formFor = tab;
  const body = $('#genbody');
  body.innerHTML = '';
  if (tab === 'Protocol') { buildProtoForm(body); return; }
  const s = parseSpec(state.signals?.[tab]);
  const o = s.opts;
  const line = isLine(s.kind);
  const wave = line ? 'sine' : s.kind;
  const glitch = (o.glitch || '').split(':');
  const burst = (o.burst || '').split(':');
  const form = h('div', { class: 'form' },
    line ? h('div', { class: 'full hint' }, `${tab} now carries a protocol line (${s.kind}); applying a waveform replaces it.`) : null,
    row('Waveform', sel('w-kind', WAVES, wave)),
    row('Frequency', inp('w-freq', fmtSI(line ? 1e3 : s.freq || 1e3)), 'Hz'),
    row('Amplitude', inp('w-vpp', num(s.vpp)), 'Vpp'),
    row('Offset', inp('w-offset', num(s.offset)), 'V'),
    row('Duty', inp('w-duty', o.duty ? num(o.duty * 100) : ''), '%'),
    row('Pulse width', inp('w-width', o.width ? fmtSI(+o.width) : ''), 's'),
    row('Rise / fall', h('div', { class: 'pair' }, inp('w-rise', o.rise ? fmtSI(+o.rise) : '', { placeholder: 'rise' }),
      inp('w-fall', o.fall ? fmtSI(+o.fall) : '', { placeholder: 'fall' })), 's'),
    row('Added noise', inp('w-noise', o.noise ? fmtSI(+o.noise) : ''), 'Vrms'),
    h('div', { class: 'sep' }),
    row('Glitch', h('div', { class: 'pair' }, inp('w-gevery', glitch[0] || '', { placeholder: 'every N', title: 'every N-th period' }),
      inp('w-gwidth', glitch[1] ? fmtSI(+glitch[1]) : '', { placeholder: 'width s' }),
      inp('w-gamp', glitch[2] || '', { placeholder: 'amp V' }))),
    row('Burst', h('div', { class: 'pair' }, inp('w-bon', burst[0] || '', { placeholder: 'on periods' }),
      inp('w-boff', burst[1] || '', { placeholder: 'off periods' }))),
    row('Invert', h('input', { id: 'w-inv', type: 'checkbox', checked: o.inv === '1' })),
  );
  body.append(form, h('div', { class: 'genfoot' },
    h('button', { class: 'ok', id: 'w-apply' }, `Apply to ${tab}`),
    h('label', { class: 'row-tight' }, h('input', { type: 'checkbox', id: 'w-live', checked: true }), 'apply as I type'),
    h('button', { class: 'sm', id: 'w-off' }, 'Off (0 V)')));
  const showRows = () => {
    const k = $('#w-kind').value;
    const vis = { 'w-duty': ['square', 'triangle', 'ramp', 'pulse'], 'w-width': ['pulse'], 'w-rise': ['square', 'pulse'] };
    for (const [id, kinds] of Object.entries(vis)) {
      const el = $(`#${id}`);
      const show = kinds.includes(k);
      el.disabled = !show;
      if (id === 'w-rise') $('#w-fall').disabled = !show;
    }
    $('#w-freq').disabled = k === 'dc' || k === 'noise';
  };
  showRows();
  let t;
  form.addEventListener('input', () => { showRows(); if ($('#w-live').checked) { clearTimeout(t); t = setTimeout(applyWave, 350); } });
  form.addEventListener('change', () => { showRows(); if ($('#w-live').checked) applyWave(); });
  $('#w-apply').addEventListener('click', applyWave);
  $('#w-off').addEventListener('click', () => sendSignal(tab, 'dc,0,0,0'));
}

function waveSpec() {
  const k = $('#w-kind').value;
  const v = (id) => $(`#${id}`).value.trim();
  const need = (id, what, opt) => {
    const s = v(id);
    if (s === '' && opt) return null;
    const x = parseSI(s);
    if (!isFinite(x)) throw new Error(`${what}: "${s}" is not a number (SI suffixes like 2.5k, 10n work)`);
    return x;
  };
  const freq = (k === 'dc' || k === 'noise') ? 0 : need('w-freq', 'frequency');
  const parts = [k, num(freq), num(need('w-vpp', 'amplitude')), num(need('w-offset', 'offset'))];
  const opt = (key, id, what, scale = 1) => {
    if ($(`#${id}`).disabled) return;
    const x = need(id, what, true);
    if (x !== null && x !== 0) parts.push(`${key}=${num(x * scale)}`);
  };
  opt('duty', 'w-duty', 'duty', 0.01);
  opt('width', 'w-width', 'pulse width');
  opt('rise', 'w-rise', 'rise');
  opt('fall', 'w-fall', 'fall');
  opt('noise', 'w-noise', 'noise');
  const ge = need('w-gevery', 'glitch every', true), gw = need('w-gwidth', 'glitch width', true), ga = need('w-gamp', 'glitch amp', true);
  if (ge && gw) parts.push(`glitch=${Math.round(ge)}:${num(gw)}:${num(ga || 0)}`);
  const bon = need('w-bon', 'burst on', true), boff = need('w-boff', 'burst off', true);
  if (bon && boff) parts.push(`burst=${Math.round(bon)}:${Math.round(boff)}`);
  if ($('#w-inv').checked) parts.push('inv=1');
  return parts.join(',');
}

async function applyWave() {
  let spec;
  try { spec = waveSpec(); } catch (e) { flash(e.message, true); return; }
  await sendSignal(tab, spec);
}

async function sendSignal(ch, spec) {
  const r = await api('/api/sim/signal', { ch, spec });
  if (r.ok) { setSignals(r.signals); flash(`${ch} ← ${spec}`); } else flash(`${ch}: ${r.err}`, true);
}

let proto = { p: 'uart', lines: { ...PROTOS.uart.def }, rate: PROTOS.uart.rate, vpp: '3.3', offset: '1.65', data: PROTOS.uart.data, fmt: 'text', idle: '', opts: {} };

function buildProtoForm(body) {
  const P = PROTOS[proto.p];
  const presets = h('div', { class: 'presets' }, PRESETS.map((pr) => h('button', { class: 'xs', onclick: () => {
    const d = PROTOS[pr.p];
    proto = { p: pr.p, lines: { ...(pr.lines || d.def) }, rate: d.rate, vpp: pr.vpp || '3.3', offset: pr.offset || '1.65', data: d.data, fmt: d.fmt, idle: '', opts: {} };
    buildForm();
    applyBus();
  } }, pr.label)));
  const lineRows = Object.entries(P.roles).map(([role, label]) =>
    row(label, sel(`p-line-${role}`, [['', '—'], ...inputs()], proto.lines[role] || '')));
  const o = proto.opts;
  const extra = {
    uart: [row('Data bits', sel('p-bits', [5, 6, 7, 8, 9], o.bits || 8)),
      row('Parity', sel('p-parity', [['n', 'none'], ['e', 'even'], ['o', 'odd'], ['m', 'mark'], ['s', 'space']], o.parity || 'n')),
      row('Stop bits', sel('p-stop', [1, 1.5, 2], o.stop || 1)),
      row('Byte gap', inp('p-gap', o.gap || ''), 's'),
      row('Bit order', sel('p-lsb', [['1', 'LSB first'], ['0', 'MSB first']], o.lsb ?? '1'))],
    spi: [row('Mode', sel('p-mode', [['0', '0 (CPOL 0, CPHA 0)'], ['1', '1 (CPOL 0, CPHA 1)'], ['2', '2 (CPOL 1, CPHA 0)'], ['3', '3 (CPOL 1, CPHA 1)']], o.mode || '0')),
      row('Bits / word', sel('p-bits', [8, 16, 24, 32], o.bits || 8)),
      row('Bit order', sel('p-lsb', [['0', 'MSB first'], ['1', 'LSB first']], o.lsb ?? '0')),
      row('MISO data', inp('p-miso', o.miso || '', { placeholder: 'hex' })),
      row('CS → clock', inp('p-csgap', o.csgap || ''), 's')],
    i2c: [row('Address', inp('p-addr', o.addr || '0x50'), '7-bit'),
      row('Direction', sel('p-rw', [['w', 'write'], ['r', 'read (target sends the data)']], o.rw || 'w'))],
    can: [row('Identifier', inp('p-id', o.id || '0x123')),
      row('Extended id', sel('p-ext', [['0', '11-bit'], ['1', '29-bit']], o.ext || '0')),
      row('Frame', sel('p-rtr', [['0', 'data'], ['1', 'remote (RTR)']], o.rtr || '0'))],
    lin: [row('Frame id', inp('p-id', o.id || '0x10'), '0…0x3F'),
      row('Checksum', sel('p-checksum', [['enh', 'enhanced (2.x)'], ['cls', 'classic (1.x)']], o.checksum || 'enh'))],
    manchester: [row('Bit order', sel('p-lsb', [['0', 'MSB first'], ['1', 'LSB first']], o.lsb ?? '0'))],
    prbs: [row('Order', sel('p-order', [[7, 'PRBS7'], [9, 'PRBS9'], [11, 'PRBS11'], [15, 'PRBS15']], o.order || 7))],
  }[proto.p];
  const form = h('div', { class: 'form' },
    row('Protocol', sel('p-kind', Object.entries(PROTOS).map(([k, v]) => [k, v.name]), proto.p)),
    ...lineRows,
    row('Bit rate', inp('p-rate', proto.rate), 'bit/s'),
    row('Levels', h('div', { class: 'pair' }, inp('p-vpp', proto.vpp, { title: 'swing (Vpp)' }), h('span', { class: 'hint' }, 'Vpp at'),
      inp('p-offset', proto.offset, { title: 'mid level (V)' }), h('span', { class: 'hint' }, 'V mid'))),
    row('Payload', h('div', { class: 'pair' }, inp('p-data', proto.data), sel('p-fmt', [['text', 'text'], ['hex', 'hex']], proto.fmt))),
    row('Idle after frame', inp('p-idle', proto.idle, { placeholder: '20 bit times' }), 's'),
    h('div', { class: 'sep' }),
    ...extra,
  );
  body.append(presets, form, h('div', { class: 'genfoot' }, h('button', { class: 'ok', id: 'p-apply' }, 'Apply protocol'),
    h('span', { class: 'hint' }, 'the frame repeats from the trigger point')));
  $('#p-kind').addEventListener('change', (e) => {
    const d = PROTOS[e.target.value];
    proto = { ...proto, p: e.target.value, lines: { ...d.def }, rate: d.rate, data: d.data, fmt: d.fmt, opts: {} };
    buildForm();
  });
  $('#p-apply').addEventListener('click', applyBus);
}

function toHex(s, fmt) {
  if (fmt === 'text') return Array.from(new TextEncoder().encode(s), (b) => b.toString(16).padStart(2, '0')).join('');
  const x = s.replace(/0x/gi, '').replace(/[\s,:_]/g, '');
  if (!/^([0-9a-fA-F]{2})*$/.test(x)) throw new Error(`payload "${s}": want hex bytes like A5 3C`);
  return x.toLowerCase();
}

function readProto() {
  const v = (id) => $(`#${id}`)?.value.trim() ?? '';
  const p = { ...proto, lines: {}, opts: {} };
  for (const role of Object.keys(PROTOS[proto.p].roles)) p.lines[role] = v(`p-line-${role}`);
  p.rate = v('p-rate'); p.vpp = v('p-vpp'); p.offset = v('p-offset'); p.data = $('#p-data').value; p.fmt = v('p-fmt'); p.idle = v('p-idle');
  for (const k of ['bits', 'parity', 'stop', 'gap', 'lsb', 'mode', 'miso', 'csgap', 'addr', 'rw', 'id', 'ext', 'rtr', 'checksum', 'order']) {
    if ($(`#p-${k}`)) p.opts[k] = v(`p-${k}`);
  }
  return p;
}

async function applyBus() {
  try {
    const p = readProto();
    proto = p;
    const used = Object.values(p.lines).filter(Boolean);
    if (!used.length) throw new Error('place at least one line on a channel');
    if (new Set(used).size !== used.length) throw new Error('two lines on one channel');
    const rate = parseSI(p.rate), vpp = parseSI(p.vpp), offset = parseSI(p.offset);
    if (!(rate > 0)) throw new Error(`bit rate "${p.rate}"`);
    if (!isFinite(vpp) || !isFinite(offset)) throw new Error('levels: numbers in V');
    const opts = p.p === 'prbs' ? {} : { data: toHex(p.data, p.fmt) };
    if (p.idle) { const x = parseSI(p.idle); if (!isFinite(x)) throw new Error(`idle "${p.idle}"`); opts.idle = num(x); }
    for (const [k, val] of Object.entries(p.opts)) {
      if (val === '') continue;
      if (k === 'gap' || k === 'csgap') { const x = parseSI(val); if (!isFinite(x)) throw new Error(`${k} "${val}"`); opts[k] = num(x); continue; }
      if (k === 'miso') { opts[k] = toHex(val, 'hex'); continue; }
      opts[k] = val;
    }
    const lines = {};
    for (const [r, c] of Object.entries(p.lines)) if (c) lines[r] = c;
    const r = await api('/api/sim/bus', { kind: p.p, lines, rate, vpp, offset, opts });
    if (!r.ok) throw new Error(r.err);
    flash(`${PROTOS[p.p].name} on ${Object.entries(lines).map(([k, c]) => `${k.toUpperCase()}=${c}`).join(' ')}`);
    setSignals(r.signals);
  } catch (e) { flash(e.message, true); }
}

function describe(spec) {
  const s = parseSpec(spec);
  if (isLine(s.kind)) {
    const P = PROTOS[protoOf(s.kind)];
    return `${P.name} ${s.kind.split('.')[1]?.toUpperCase() || ''} ${fmtSI(s.freq)}bit/s`;
  }
  if (s.kind === 'dc') return `DC ${num(s.offset)} V`;
  if (s.kind === 'noise') return `noise σ ${num(s.vpp / 6)} V`;
  return `${s.kind} ${fmtSI(s.freq)}Hz ${num(s.vpp)} Vpp ${s.offset ? `${num(s.offset)} V offset` : ''}`;
}

function renderSigTable() {
  const tb = $('#sigtable tbody');
  tb.innerHTML = '';
  for (const c of inputs()) {
    const spec = state.signals?.[c] || '';
    tb.append(h('tr', {}, h('td', { class: `cc${c[1]}`, style: `color: var(--c${c[1]})` }, c),
      h('td', {}, describe(spec), h('div', { class: 'spec' }, spec))));
  }
}

// ---------------------------------------------------------------- state
function setSignals(list) {
  if (!Array.isArray(list)) return;
  state.signals = {};
  list.forEach((s, i) => { state.signals[`C${i + 1}`] = s; });
  renderSigTable();
}

let polling = false;
async function poll() {
  if (polling) return;
  polling = true;
  try {
    const st = await api('/api/sim/state');
    if (st.ok) {
      const first = formFor === null;
      channels = st.channels || channels;
      setSignals(st.signals);
      if (site.mode === 'native') showLamps(st.lamps);
      const chip = $('#runchip');
      chip.className = 'chip running';
      chip.textContent = 'running';
      if (first) { buildTabs(); buildForm(); }
    }
  } catch { /* the firmware is busy or gone: keep trying */ }
  polling = false;
}

function tickStats() {
  const now = performance.now();
  const dt = (now - stats.fpsAt) / 1000;
  if (dt >= 2) {
    stats.fps = stats.frames / dt;
    stats.frames = 0;
    stats.fpsAt = now;
    const load = stats.loadMs ? `module ${Math.round(stats.loadMs)} ms · ` : '';
    $('#runinfo').textContent = `${load}first picture ${(stats.firstFrameMs / 1000).toFixed(1)} s · screen ${stats.fps.toFixed(0)} fps`;
  }
}

async function main() {
  try { site = await fetch('mode.json').then((r) => r.json()); } catch { /* the static site: wasm */ }
  demo = await fetch('scope/demo.json').then((r) => r.json());
  document.title = `${demo.name} demo`;
  $('#brandname').textContent = demo.name;
  $('#plate').textContent = demo.name;
  channels = demo.channels || channels;
  if (demo.screen) {
    $('#screenwrap').style.setProperty('--screen-aspect', `${demo.screen.w} / ${demo.screen.h}`);
    $('#screen').width = demo.screen.w;
    $('#screen').height = demo.screen.h;
  }
  buildPanel(demo.panel || {});
  wireScreen();
  $('#reset').addEventListener('click', () => {
    const pre = storePrefix();
    for (const k of Object.keys(localStorage)) if (k.startsWith(pre)) localStorage.removeItem(k);
    location.reload();
  });
  try {
    if (site.mode === 'native') pollScreen();
    else await startWasm();
  } catch (e) {
    $('#runchip').className = 'chip ended';
    $('#runchip').textContent = 'stopped';
    $('#screenmsg').textContent = String(e.message || e);
    return;
  }
  for (const a of ['#weblink', '#weblink2']) $(a).href = site.ui;
  $('#webframe').src = site.ui;
  $('#webreload').addEventListener('click', () => { $('#webframe').src = site.ui; });
  poll();
  setInterval(poll, 1000);
  setInterval(tickStats, 500);
}

main();
