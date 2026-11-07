// ENGMODEL-OWNER-UNIT: FU-DSO-WEBCLIENT
// The page's parts that follow a scope of more channels than two, run over
// a four-channel scope: SOUND names its lanes from the stream's scales, BUS
// Visual reads a record of any channels, a reference holds every channel it
// was stored with, super-res puts a stack on its lanes' channels, and the
// FFT keeps a peak list per channel. Run by TestCapsNJS (caps_n_node_test.go).
"use strict";
const assert = require("node:assert/strict"), fs = require("node:fs"), vm = require("node:vm");
// TRLC-LINKS: REQ-DSO-204
const src = (f) => fs.readFileSync(f, "utf8");
// TRLC-LINKS: REQ-DSO-204
const fn = (text, name) => { const i = text.indexOf("function " + name + "("); return text.slice(text.lastIndexOf("\n", i - 7), text.indexOf("\n}\n", i) + 3); };
// TRLC-LINKS: REQ-DSO-204
const json = (v) => JSON.parse(JSON.stringify(v));
// TRLC-LINKS: REQ-DSO-204
function ctx(extra) {
  const c = Object.assign({ view: { c1: true, c2: true, c3: true, c4: true }, st: null, document: undefined, TextDecoder, Uint8Array, Float32Array }, extra);
  vm.createContext(c);
  vm.runInContext(src("caps.js"), c);
  vm.runInContext('caps = capsNormalize({ channels: [1, 2, 3, 4].map((n) => ({ ch: n - 1, name: "C" + n })) }); capsServed = true;', c);
  return c;
}

// SOUND: a reply's newest scale names the lanes' channels; a reply of no
// samples names them too, and a scope of two channels names none
{
  const sc = src("sound_client.js");
  const c = ctx({});
  vm.runInContext(fn(sc, "sndLanesOf"), c);
  const cl = {};
  c.sndLanesOf(cl, { scales: [{ from: 0, vpc: [1, 1], off: [0, 0] }] });
  assert.equal(cl.lanes, undefined, "two channels: no lanes named");
  c.sndLanesOf(cl, { n: 0, scales: [{ from: 0, vpc: [1, 1], off: [0, 0], chans: [0, 1] }, { from: 9, vpc: [1, 1], off: [0, 0], chans: [3, 0] }] });
  assert.deepEqual(json(cl.lanes), [3, 0], "the newest scale's lanes");
  const as = src("app_sound.js");
  const lanes = as.slice(as.indexOf("// sndLanes is the channel index"), as.indexOf("let sndChansSig"));
  c.snd = { lanes: [3, 0] };
  // TRLC-LINKS: REQ-DSO-1004
  c.chanColor = (n) => "col" + n;
  vm.runInContext(lanes, c);
  assert.equal(c.sndLaneOf(3), 0, "C4 is on lane 0");
  assert.equal(c.sndLaneOf(0), 1, "C1 on lane 1");
  assert.equal(c.sndLaneOf(2), 0, "C3 is in no lane: the first");
  assert.equal(c.sndLaneName(0), "C4");
  c.snd = {};
  assert.equal(c.sndLaneOf(1), 1, "no lanes named: C1 and C2");
}

// BUS Visual: a record names its channels (chans), else C1 and C2
{
  const va = src("app_visual_audio.js");
  // TRLC-LINKS: REQ-DSO-280
  const rec = (h, n) => {
    const hj = Buffer.from(JSON.stringify(h)), b = Buffer.alloc(4 + hj.length + n);
    b.writeUInt32LE(hj.length, 0); hj.copy(b, 4);
    for (let i = 0; i < n; i++) b[4 + hj.length + i] = i;
    return b.buffer.slice(b.byteOffset, b.byteOffset + b.length);
  };
  for (const [h, want] of [[{ n: 2, sample_s: 1e-6 }, { c1: [0, 1], c2: [2, 3] }], [{ n: 2, sample_s: 1e-6, chans: [0, 2, 3] }, { c1: [0, 1], c3: [2, 3], c4: [4, 5] }]]) {
    const c = ctx({
      // TRLC-LINKS: REQ-DSO-1004
      fetch: async () => ({
        ok: true,
        // TRLC-LINKS: REQ-DSO-1004
        arrayBuffer: async () => rec(h, 6),
      }),
    });
    vm.runInContext(fn(va, "visFetchRecord"), c);
    c.visFetchRecord(1e-6).then((r) => {
      for (const [k, v] of Object.entries(want)) assert.deepEqual(json(r[k]), v, "record " + k);
    });
  }
}

// references: every channel the export holds, each with its own scale
{
  const cmp = src("app_compare.js");
  const c = ctx({
    refs: { A: null, B: null },
    // TRLC-LINKS: REQ-DSO-1004
    updateRefRows() {},
    // TRLC-LINKS: REQ-DSO-1004
    redraw() {},
  });
  vm.runInContext(fn(cmp, "refKeys") + fn(cmp, "refChans") + fn(cmp, "loadScopeRef"), c);
  c.loadScopeRef("A", { has: true, gen: 1, cols: 2, c1: [100, 101], c3: [120, 121], c4: [130, 131], vdiv: [1, 1, 0.5, 2], off: [0, 0, 0.1, 0], probe: [1, 1, 1, 1], tdiv_s: 1e-6, pos_frac: 0.5 });
  const r = c.refs.A;
  assert.deepEqual(json(c.refChans(r)), [1, 3, 4], "C1, C3 and C4 held");
  assert.equal(r.vpc3, 0.02); assert.equal(r.off3, 0.1); assert.equal(r.vdiv4, 2);
  assert.equal(r.c2, null, "C2 not held");
  c.loadScopeRef("B", { has: true, gen: 2, cols: 1, c1: [1], c2: [2], vdiv: [1, 1], off: [0, 0], tdiv_s: 1e-6, pos_frac: 0.5 });
  assert.deepEqual(json(c.refChans(c.refs.B)), [1, 2], "a two-channel export");
}

// super-res: a stack's view frame on its lanes' channels
{
  const sr = src("app_superres.js");
  const c = ctx({ sr: {} });
  vm.runInContext(sr.slice(sr.indexOf("// sr.lanes are the channels"), sr.indexOf("// ==== ETS")), c);
  const f = { c1: "a", c2: "b", vpc1: 1, vpc2: 2, off1_v: 0, off2_v: 3, m1: "m1", m2: "m2", clip1: false, clip2: true, seq: 4 };
  c.srRelabel(f);
  assert.equal(f.c3, undefined, "lanes C1 and C2: as built");
  c.sr.lanes = [1, 4];
  c.srRelabel(f);
  assert.deepEqual([f.c1, f.c4, f.vpc4, f.off4_v, f.m4, f.clip4, f.c2, f.seq], ["a", "b", 2, 3, "m2", true, undefined, 4], "lane 1 is C4");
  assert.equal(c.srL({ c4: "x" }, 1), "x");
}

// FFT: a peak list, spectrum memo and palette per channel
{
  const ff = src("app_fft.js");
  const c = ctx({ fftCh: { 1: { sel: [] }, 2: { sel: [] } }, specMemo: { 1: {}, 2: {} }, COMPCOLS: { 1: ["w"], 2: ["c"] }, CAPS_HOOKS: [] });
  vm.runInContext(fn(ff, "fftChans"), c);
  c.fftChans();
  assert.ok(c.fftCh[4] && c.specMemo[4] && c.COMPCOLS[3][0] === "w" && c.COMPCOLS[4][0] === "c", "C3 and C4 have theirs");
}

// trigger level: volts by the source's channel where caps list the control
{
  const sent = [];
  const c = ctx({
    // TRLC-LINKS: REQ-DSO-1004
    fetch: async (u, o) => {
      sent.push(o.body);
      return {
        // TRLC-LINKS: REQ-DSO-1004
        json: async () => ({ ok: true }),
      };
    },
    autosetBusy: false,
    // TRLC-LINKS: REQ-DSO-1004
    kickStatus() {},
    // TRLC-LINKS: REQ-DSO-1004
    toast() {},
    st: { trig_source: 2 },
  });
  const ctl = src("app_controls.js");
  vm.runInContext(fn(ctl, "sendBody") + fn(ctl, "sendTrigLevel"), c);
  vm.runInContext('caps = capsNormalize({ channels: caps.channels, trigger: { sources: ["C1", "C2", "C3", "C4"] }, controls: [{ name: "triglevel", unit: "V" }] });', c);
  c.sendTrigLevel(0.3).then(() => assert.deepEqual(sent, ['{"control":"triglevel","ch":2,"value_v":0.3}'], "C3's level in volts"));
}
process.on("exit", (code) => { if (code === 0) console.log("ALL PASS"); });
