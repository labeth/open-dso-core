// ENGMODEL-OWNER-UNIT: FU-DSO-WEBCLIENT
// TRLC-LINKS: REQ-DSO-069
// The CSV export of a channel displayed inverted (Cn:INVS) holds what the
// screen shows: its volts negated, the header naming it; a channel not
// inverted exports as acquired.
"use strict";
const assert = require('node:assert/strict'), fs = require('node:fs'), vm = require('node:vm');
const src = fs.readFileSync('app_controls.js', 'utf8');
const csv = src.slice(src.indexOf('$("eCSV").onclick'), src.indexOf('// ---- sigrok exports'));
const inv = src.slice(src.indexOf('// exportInvert is'), src.indexOf('// TRLC-LINKS: REQ-DSO-069\nfunction exportName('));
// TRLC-LINKS: REQ-DSO-069
function exportCSV(st) {
  let out = null;
  const handlers = {};
  const c = {
    st, frame: { seq: 4, c1: [153, 128], c2: [153, 128], dt_s: 1e-6, vpc1: 0.04, vpc2: 0.04, edge_frac: -1 },
    // TRLC-LINKS: REQ-DSO-069
    $: (id) => (handlers[id] = handlers[id] || {}),
    // TRLC-LINKS: REQ-DSO-069
    exportName: (seq, ext) => 'scope-' + seq + '.' + ext,
    // TRLC-LINKS: REQ-DSO-069
    exportFile: (name, type, data) => { out = data; },
  };
  vm.createContext(c);
  vm.runInContext(fs.readFileSync('caps.js', 'utf8'), c);
  vm.runInContext(inv + '\n' + csv, c);
  handlers.eCSV.onclick();
  return out.trim().split('\n');
}
const plain = exportCSV({ inv1: false, inv2: false });
assert.ok(!/inverted/.test(plain[0]), 'no invert: no note');
assert.equal(plain[2], '0.000000e+0,1.000000e+0,1.000000e+0');
const shown = exportCSV({ inv1: true, inv2: false });
assert.match(shown[0], / inverted=c1_as_displayed/);
assert.equal(shown[2], '0.000000e+0,-1.000000e+0,1.000000e+0', 'C1 negated, C2 as acquired');
// TRLC-LINKS: REQ-DSO-069
function exportCSV4(st) {
  let out = null;
  const handlers = {};
  const c = {
    st, frame: { seq: 5, c1: [153, 128], c2: [128, 128], c3: Float32Array.from([128.5, 128]), c4: [103, 128], dt_s: 1e-6,
      vpc1: 0.04, vpc2: 0.04, vpc3: 0.02, vpc4: 0.04, off4_v: 0.5, edge_frac: -1 },
    // TRLC-LINKS: REQ-DSO-069
    $: (id) => (handlers[id] = handlers[id] || {}),
    // TRLC-LINKS: REQ-DSO-069
    exportName: (seq, ext) => 'scope-' + seq + '.' + ext,
    // TRLC-LINKS: REQ-DSO-069
    exportFile: (name, type, data) => { out = data; },
  };
  vm.createContext(c);
  vm.runInContext(fs.readFileSync('caps.js', 'utf8'), c);
  vm.runInContext('caps = capsNormalize({ channels: [1, 2, 3, 4].map((n) => ({ ch: n - 1, name: "C" + n })) }); capsServed = true;', c);
  vm.runInContext(inv + '\n' + csv, c);
  handlers.eCSV.onclick();
  return out.trim().split('\n');
}
const four = exportCSV4({ inv4: true, probe3: 10 });
assert.equal(four[1], 't_s,c1_v,c2_v,c3_v,c4_v', 'a column per channel');
assert.match(four[0], / probe_c3=10 probe_c4=1/);
assert.match(four[0], / inverted=c4_as_displayed/);
assert.equal(four[2], '0.000000e+0,1.000000e+0,0.000000e+0,1.000000e-2,1.500000e+0', 'C3 a fractional 12-bit unit, C4 inverted with its offset');
console.log('ALL PASS');
