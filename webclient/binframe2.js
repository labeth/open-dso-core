// ENGMODEL-OWNER-UNIT: FU-WEB-BINFRAME
"use strict";
// binframe2.js — decoder for frame wire v2 (/api/frame.bin?v=2; layout doc
// beside wire.EncodeFrame2): a "DSF2" magic, a u32 header length, the JSON
// header, padding to 8 bytes, then the segments the header lists, each a
// typed array viewed in place.
//
// decodeFrameV2 gives {...header, traces: [{ch, fmt, scale, coupling, clip,
// head, tail, meas, codes|min|max|view: TypedArray}]}: the words exactly as
// the scope stored them (Uint8Array or Uint16Array), any channel count.
//
// frameDisplay turns that into the frame object every consumer of the page
// reads: per channel n (1-based) c<n> (or e<n>min/e<n>max for peak pairs,
// vc<n> for a view window), vpc<n>, off<n>_v, m<n>, clip<n>, in the display
// domain the drawing works in — 25 units a division about 128, the centre
// line, at the probe tip's volts = (unit − 128)·vpc − off. A two-channel
// 8-bit frame of that scale maps word for word (Int16Array, as v1 gives);
// any other format maps to Float32Array with nothing rounded away, which is
// how the page already carries precision words. traces[] stays on the
// object for consumers that read stored words and their scale.
//
// Every call allocates fresh arrays (captures and references hold frames by
// reference). A protocol mismatch returns null, as decodeBinFrame does.

const V2_MAGIC = [0x44, 0x53, 0x46, 0x32]; // "DSF2"

// DISPLAY_CPD and DISPLAY_MID are the display domain the drawing code works in.
const DISPLAY_CPD = 25, DISPLAY_MID = 128;

// TRLC-LINKS: REQ-SDS-023
function isFrameV2(buf) {
  const u8 = new Uint8Array(buf, 0, Math.min(4, buf.byteLength));
  return u8.length === 4 && u8[0] === V2_MAGIC[0] && u8[1] === V2_MAGIC[1] && u8[2] === V2_MAGIC[2] && u8[3] === V2_MAGIC[3];
}

// TRLC-LINKS: REQ-SDS-023
function decodeFrameV2(buf) {
  if (!buf || buf.byteLength < 8 || !isFrameV2(buf)) return null;
  const u8 = new Uint8Array(buf);
  const hlen = new DataView(buf).getUint32(4, true);
  if (hlen < 2 || 8 + hlen > u8.length) return null;
  let h;
  try {
    h = JSON.parse(new TextDecoder().decode(u8.subarray(8, 8 + hlen)));
  } catch (e) {
    return null;
  }
  if (typeof h !== "object" || h === null || typeof h.seq !== "number") return null;
  const start = (8 + hlen + 7) & ~7;
  if (h.unchanged) return u8.length <= start && !(h.segs && h.segs.length) ? h : null;
  const traces = Array.isArray(h.traces) ? h.traces : [];
  const byCh = new Map();
  for (const t of traces) {
    if (typeof t !== "object" || t === null || !(t.ch >= 0) || byCh.has(t.ch)) return null;
    if (!t.fmt || !t.scale || !(t.scale.codes_per_div > 0)) return null;
    byCh.set(t.ch, t);
  }
  for (const s of Array.isArray(h.segs) ? h.segs : []) {
    const t = byCh.get(s.ch);
    if (!t || (s.storage !== "u8" && s.storage !== "u16") || s.storage !== t.fmt.storage) return null;
    const size = s.storage === "u16" ? 2 : 1, n = s.n | 0, at = s.at | 0;
    if (n < 0 || at < 0 || at % size || start + at + n * size > u8.length) return null;
    if (!["codes", "min", "max", "view", "raw"].includes(s.role) || t[s.role]) return null;
    // a view in place: the payload starts on 8 bytes and each segment on its word size
    t[s.role] = size === 2 ? new Uint16Array(buf, start + at, n) : new Uint8Array(buf, start + at, n);
  }
  h.traces = traces;
  return h;
}

// identityScale reports a trace whose words are the display domain itself.
// TRLC-LINKS: REQ-SDS-023
function identityScale(t) {
  return t.fmt.storage === "u8" && !t.fmt.frac && t.scale.zero === DISPLAY_MID && t.scale.codes_per_div === DISPLAY_CPD;
}

// toDisplay maps a trace's words into the display domain over len columns,
// head of them before the words (-1, as v1's margins).
// TRLC-LINKS: REQ-SDS-023
function toDisplay(t, words, len, head) {
  const exact = identityScale(t);
  const out = exact ? new Int16Array(len) : new Float32Array(len);
  if (len > words.length) out.fill(-1);
  if (exact) { out.set(words, head); return out; }
  const k = DISPLAY_CPD / t.scale.codes_per_div, z = t.scale.zero;
  for (let i = 0; i < words.length; i++) out[head + i] = DISPLAY_MID + (words[i] - z) * k;
  return out;
}

// frameDisplay is the page's frame object of a decoded v2 frame (see the
// file comment); null when its margins disagree with its columns.
// TRLC-LINKS: REQ-SDS-023
function frameDisplay(h) {
  if (!h || h.unchanged) return h;
  const f = Object.assign({}, h);
  delete f.segs;
  f.wire = 2;
  if (h.content === "peak-pairs") { f.is_env = true; f.peak_detect = true; }
  if (h.content === "raw-full-rate") f.full_rate = true;
  if (Array.isArray(h.flags) && h.flags.includes("ordered")) f.ordered = true;
  if (h.capture_s > 0 && h.capture_s !== h.sample_s) f.capture_sample_s = h.capture_s;
  if (h.trig_kind) f.trigger_kind = h.trig_kind;
  if (h.acq_decim > 1) f.decimation = h.acq_decim;
  let cols = h.cols | 0;
  for (const t of h.traces) {
    const n = t.ch + 1, head = t.head | 0, tail = t.tail | 0;
    const words = t.codes || t.raw;
    if (words) {
      const len = head + words.length + tail;
      if (cols > 0 && len !== cols && !h.raw) return null;
      f["c" + n] = toDisplay(t, words, len, head);
      if (!(cols > 0)) cols = len;
    }
    if (t.min && t.max) {
      if (t.min.length !== t.max.length) return null;
      f["e" + n + "min"] = toDisplay(t, t.min, t.min.length, 0);
      f["e" + n + "max"] = toDisplay(t, t.max, t.max.length, 0);
    }
    if (t.view) f["vc" + n] = toDisplay(t, t.view, t.view.length, 0);
    if (t.fmt.frac > 0) f.fraction_bits = t.fmt.frac;
    // volts per display unit, so (unit − 128)·vpc − off reads the tip's volts
    f["vpc" + n] = t.scale.per_code_v * t.scale.codes_per_div / DISPLAY_CPD;
    f["off" + n + "_v"] = t.scale.off_v || 0;
    if (t.meas) f["m" + n] = t.meas;
    if (t.clip) f["clip" + n] = true;
  }
  f.cols = cols;
  return f;
}

// decodeAnyFrame decodes a frame reply of either wire, told by its magic,
// into the page's frame object.
// TRLC-LINKS: REQ-SDS-023
function decodeAnyFrame(buf) {
  if (isFrameV2(buf)) return frameDisplay(decodeFrameV2(buf));
  return (typeof decodeBinFrame === "function" ? decodeBinFrame : require("./binframe.js").decodeBinFrame)(buf);
}

if (typeof module !== "undefined") module.exports = { decodeFrameV2, frameDisplay, decodeAnyFrame, isFrameV2, V2_MAGIC };
