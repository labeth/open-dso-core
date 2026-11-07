// ENGMODEL-OWNER-UNIT: FU-WEB-BINFRAME
// Node tests for binframe2.js. With no argument it checks hand-built v2
// messages; with a directory it decodes every *.bin there (frames the Go
// encoder wrote) and prints one JSON line per file for the Go side to
// compare: wire.TestFrameV2NodeRoundTrip.
"use strict";
const fs = require("fs");
const path = require("path");
const { decodeBinFrame, BIN_MAGIC } = require("./binframe.js");
const { decodeFrameV2, frameDisplay, decodeAnyFrame } = require("./binframe2.js");

// summary is a decoded frame as plain JSON: typed arrays as {type, v}.
// TRLC-LINKS: REQ-SDS-163
function summary(f) {
  if (f === null || f === undefined) return null;
  const out = {};
  for (const k of Object.keys(f).sort()) {
    const v = f[k];
    if (ArrayBuffer.isView(v)) out[k] = { type: v.constructor.name, v: Array.from(v) };
    else if (k === "traces") out[k] = v.map(summary);
    else out[k] = v;
  }
  return out;
}

if (process.argv[2]) {
  const dir = process.argv[2];
  for (const name of fs.readdirSync(dir).filter((n) => n.endsWith(".bin")).sort()) {
    const b = fs.readFileSync(path.join(dir, name));
    const buf = b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength);
    console.log(JSON.stringify({ name, frame: summary(decodeAnyFrame(buf)) }));
  }
  process.exit(0);
}

let fails = 0;
// TRLC-LINKS: REQ-SDS-163
function check(name, ok) {
  if (!ok) { console.log("FAIL " + name); fails++; } else console.log("ok   " + name);
}

// v2 builds a v2 message: header (segs are placed here), padded payload.
// TRLC-LINKS: REQ-SDS-163
function v2(header, segs) {
  let at = 0;
  header.segs = segs.map((s) => {
    const size = s.storage === "u16" ? 2 : 1, seg = { ch: s.ch, role: s.role, storage: s.storage, n: s.words.length, at };
    at = (at + s.words.length * size + 7) & ~7;
    return seg;
  });
  const hj = new TextEncoder().encode(JSON.stringify(header));
  const start = (8 + hj.length + 7) & ~7;
  const buf = new ArrayBuffer(start + at);
  const u8 = new Uint8Array(buf);
  u8.set([0x44, 0x53, 0x46, 0x32]);
  new DataView(buf).setUint32(4, hj.length, true);
  u8.set(hj, 8);
  segs.forEach((s, i) => {
    const o = start + header.segs[i].at;
    if (s.storage === "u16") new Uint16Array(buf, o, s.words.length).set(s.words);
    else u8.set(s.words, o);
  });
  return buf;
}

// msg1 builds a v1 message.
// TRLC-LINKS: REQ-SDS-163
function msg1(flags, hdr, payload) {
  const hj = new TextEncoder().encode(JSON.stringify(hdr));
  const buf = new Uint8Array(8 + hj.length + payload.length);
  buf[0] = BIN_MAGIC; buf[1] = flags;
  new DataView(buf.buffer).setUint32(4, hj.length, true);
  buf.set(hj, 8); buf.set(payload, 8 + hj.length);
  return buf.buffer;
}

const fmt8 = { bits: 8, storage: "u8", shift: 0, frac: 0, mid: 128, min: 0, max: 255 };
const sc8 = (vpc, off) => ({ zero: 128, per_code_v: vpc, off_v: off, codes_per_div: 25 });

// A two-channel 8-bit v2 frame reads as the same object as its v1 twin.
{
  const c1 = [100, 128, 156, 200], c2 = [10, 20, 30, 40];
  const one = decodeBinFrame(msg1(0, { seq: 5, cols: 4, vpc1: 0.04, vpc2: 0.02, off1_v: 0.1, off2_v: 0, edge_x: 2 }, new Uint8Array(c1.concat(c2))));
  const two = decodeAnyFrame(v2({
    seq: 5, cols: 4, edge_x: 2, trigger_at: 2,
    traces: [{ ch: 0, fmt: fmt8, scale: sc8(0.04, 0.1) }, { ch: 1, fmt: fmt8, scale: sc8(0.02, 0) }],
  }, [{ ch: 0, role: "codes", storage: "u8", words: c1 }, { ch: 1, role: "codes", storage: "u8", words: c2 }]));
  check("v2 8-bit decodes", !!two && two.wire === 2);
  for (const k of ["c1", "c2"]) check("v2 " + k + " = v1 " + k, two && two[k] instanceof Int16Array && [...two[k]].join() === [...one[k]].join());
  for (const k of ["vpc1", "vpc2", "off1_v", "off2_v", "seq", "cols"]) check("v2 " + k + " = v1", two && Math.abs(two[k] - one[k]) < 1e-15);
}

// Margins come back as -1, as v1's.
{
  const f = decodeAnyFrame(v2({ seq: 6, cols: 6, depth: 6, traces: [{ ch: 0, fmt: fmt8, scale: sc8(0.04, 0), head: 2, tail: 1 }] },
    [{ ch: 0, role: "codes", storage: "u8", words: [7, 8, 9] }]));
  check("margins", f && [...f.c1].join() === "-1,-1,7,8,9,-1" && f.c2 === undefined);
}

// Four channels of 12-bit codes left-aligned in u16 about 0x8000, 7680 a
// division: display units 128 + (w − 0x8000)/7680·25, nothing rounded.
const fmt12 = { bits: 12, storage: "u16", shift: 4, frac: 0, mid: 0x8000, min: 0x0800, max: 0xf7f0 };
const sc12 = (vdiv, off) => ({ zero: 0x8000, per_code_v: vdiv / 7680, off_v: off, codes_per_div: 7680 });
{
  const w = [0x8000, 0x8000 + 16, 0x8000 + 7680, 0x8000 - 3 * 7680];
  const traces = [0, 1, 2, 3].map((ch) => ({ ch, fmt: fmt12, scale: sc12(0.5 * (ch + 1), 0.01 * ch), meas: { vpp: ch } }));
  const f = decodeAnyFrame(v2({ seq: 7, cols: 4, content: "raw-full-rate", traces }, [0, 1, 2, 3].map((ch) => ({ ch, role: "codes", storage: "u16", words: w }))));
  check("12-bit decodes 4 channels", f && f.c1 && f.c2 && f.c3 && f.c4 && f.c4 instanceof Float32Array);
  check("12-bit display units", f && Math.abs(f.c3[0] - 128) < 1e-9 && Math.abs(f.c3[1] - (128 + 16 * 25 / 7680)) < 1e-4 && f.c3[2] === 153 && f.c3[3] === 53);
  check("12-bit volts per unit", f && Math.abs(f.vpc4 - 2 / 25) < 1e-15 && Math.abs((f.c4[2] - 128) * f.vpc4 - 2) < 1e-6);
  check("12-bit offsets and meas", f && f.off3_v === 0.02 && f.m4.vpp === 3 && f.full_rate === true);
  const raw = decodeFrameV2(v2({ seq: 7, cols: 4, traces }, [{ ch: 2, role: "codes", storage: "u16", words: w }]));
  check("words viewed in place", raw && raw.traces[2].codes instanceof Uint16Array && raw.traces[2].codes[1] === 0x8010);
}

// Peak pairs and a view window.
{
  const f = decodeAnyFrame(v2({ seq: 8, cols: 2, content: "peak-pairs", flags: ["ordered"], view_n: 3, traces: [{ ch: 1, fmt: fmt8, scale: sc8(0.04, 0) }] },
    [{ ch: 1, role: "min", storage: "u8", words: [1, 2] }, { ch: 1, role: "max", storage: "u8", words: [3, 4] }, { ch: 1, role: "view", storage: "u8", words: [5, 6, 7] }]));
  check("peak pairs", f && f.is_env && f.peak_detect && f.ordered && [...f.e2min].join() === "1,2" && [...f.e2max].join() === "3,4");
  check("view window", f && [...f.vc2].join() === "5,6,7" && f.e1min === undefined);
}

// Precision words (u16, frac 8): the same values v1's precision frame gives.
{
  const q = [128 << 8, (129 << 8) + 64];
  const fmtq = { bits: 8, storage: "u16", shift: 0, frac: 8, mid: 128 << 8, min: 0, max: 255 << 8 };
  const f = decodeAnyFrame(v2({ seq: 9, cols: 2, traces: [{ ch: 0, fmt: fmtq, scale: { zero: 32768, per_code_v: 0.04 / 256, off_v: 0, codes_per_div: 6400 } }] },
    [{ ch: 0, role: "codes", storage: "u16", words: q }]));
  check("precision", f && f.c1 instanceof Float32Array && f.c1[0] === 128 && f.c1[1] === 129.25 && f.fraction_bits === 8 && Math.abs(f.vpc1 - 0.04) < 1e-15);
}

// Unchanged and protocol failures.
{
  const u = decodeAnyFrame(v2({ seq: 10, unchanged: true }, []));
  check("unchanged", u && u.unchanged && u.seq === 10);
  const good = v2({ seq: 11, cols: 2, traces: [{ ch: 0, fmt: fmt8, scale: sc8(0.04, 0) }] }, [{ ch: 0, role: "codes", storage: "u8", words: [1, 2] }]);
  check("good", decodeAnyFrame(good) !== null);
  check("truncated", decodeAnyFrame(good.slice(0, good.byteLength - 8)) === null);
  const bad = new Uint8Array(good.slice(0)); bad[3] = 0x33;
  check("bad magic", decodeFrameV2(bad.buffer) === null);
  check("storage mismatch", decodeAnyFrame(v2({ seq: 12, cols: 2, traces: [{ ch: 0, fmt: fmt8, scale: sc8(0.04, 0) }] },
    [{ ch: 0, role: "codes", storage: "u16", words: [1, 2] }])) === null);
  check("segment of no trace", decodeAnyFrame(v2({ seq: 12, cols: 2, traces: [] }, [{ ch: 3, role: "codes", storage: "u8", words: [1, 2] }])) === null);
  check("columns disagree", decodeAnyFrame(v2({ seq: 12, cols: 5, traces: [{ ch: 0, fmt: fmt8, scale: sc8(0.04, 0) }] },
    [{ ch: 0, role: "codes", storage: "u8", words: [1, 2] }])) === null);
  check("no scale", decodeAnyFrame(v2({ seq: 12, cols: 2, traces: [{ ch: 0, fmt: fmt8 }] }, [])) === null);
  check("v1 still decodes", decodeAnyFrame(msg1(0, { seq: 5, cols: 2 }, new Uint8Array([1, 2, 3, 4]))).c2[1] === 4);
  check("display of null", frameDisplay(null) === null);
}

console.log(fails ? fails + " FAILED" : "ALL PASS");
process.exit(fails ? 1 : 0);
