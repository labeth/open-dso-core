// ENGMODEL-OWNER-UNIT: FU-WEB-APP
// caps.js — the scope's capabilities (GET /api/caps) as the page reads them,
// and the per-channel helpers every part of the page draws and lists
// channels with (classic script; loaded after app.js and app_ui.js).
//
// A scope that serves caps puts "caps_gen" in its status; the page reads
// /api/caps then, and again when caps_gen moves (an image switch, a
// variant or probe change that moves a ladder). A scope that serves none
// is the two-channel 8-bit scope the page was written for: CAPS_FALLBACK
// is exactly what the page assumed of it, so its page asks for nothing
// more and shows nothing different.
"use strict";

// CAPS_FALLBACK is what the page assumes of a scope without caps: two
// channels of 8-bit codes, 25 codes a division about code 128, frames in
// wire v1, the four trigger types on C1 or C2.
const CAPS_FALLBACK = Object.freeze({
  api: 1, caps_gen: 0,
  wire: { frame: [1] },
  channels: [{ ch: 0, name: "C1", color: "c1" }, { ch: 1, name: "C2", color: "c2" }],
  codes: { bits: 8, storage: "u8", shift: 0, frac: 0, min: 0, max: 255, mid: 128, codes_per_div: 25, frac_bits_precision: 8 },
  trigger: { types: ["edge", "pulse", "slope", "video"], sources: ["C1", "C2"] },
  features: {},
});

// TRIG_TYPES are the page's trigger types in the order of its #ttype values.
const TRIG_TYPES = ["edge", "pulse", "slope", "video"];

let caps = CAPS_FALLBACK;  // the scope's capabilities, or the fallback
let capsServed = false;    // the scope served caps
let capsGenSeen;           // the caps_gen last read
let capsBusy = false;

// chanNums is the channels as the page numbers them (1-based, as c1, vpc1, tC1).
// TRLC-LINKS: REQ-SDS-204
function chanNums() { return caps.channels.map((c) => c.ch + 1); }

// chanName is channel n's name ("C3").
// TRLC-LINKS: REQ-SDS-204
function chanName(n) {
  const c = caps.channels[n - 1];
  return (c && c.name) || "C" + n;
}

// chanColorCache holds each channel's colour token as computed once.
const chanColorCache = {};
// CHAN_COLOR_FALLBACK colours a channel past the tokens a stylesheet defines.
const CHAN_COLOR_FALLBACK = ["#f5d90a", "#f06cc4", "#4fa8ff", "#5fd35f", "#ff9f1c", "#b98cff", "#35c8e8", "#e8604c"];
// chanColor is channel n's colour: its design token (c1..cN).
// TRLC-LINKS: REQ-SDS-204
function chanColor(n) {
  if (n === 1 && typeof C1COL !== "undefined") return C1COL;
  if (n === 2 && typeof C2COL !== "undefined") return C2COL;
  if (chanColorCache[n]) return chanColorCache[n];
  const c = caps.channels[n - 1], tok = (c && c.color) || "c" + n;
  const v = getComputedStyle(document.body).getPropertyValue("--" + tok).trim() || CHAN_COLOR_FALLBACK[(n - 1) % CHAN_COLOR_FALLBACK.length];
  return (chanColorCache[n] = v);
}

// chanArr is frame f's display-domain trace of channel n (c<n>).
// TRLC-LINKS: REQ-SDS-204
function chanArr(f, n) { return f ? f["c" + n] : undefined; }

// chanVpc and chanOff are frame f's volts per display unit and offset of
// channel n, so (unit − 128)·vpc − off reads the tip's volts.
// TRLC-LINKS: REQ-SDS-204
function chanVpc(f, n) { return (f && f["vpc" + n]) || 1 / 25; }
// TRLC-LINKS: REQ-SDS-204
function chanOff(f, n) { return (f && f["off" + n + "_v"]) || 0; }

// chanShown is whether channel n is drawn.
// TRLC-LINKS: REQ-SDS-204
function chanShown(n) { return !!view["c" + n]; }

// rawFrame reads one reply of the raw feed (/api/frame.bin?raw=1&…): in
// frame wire v2 when the scope lists it, so an analyzer reads every channel
// in the page's display domain whatever the scope's code format; else v1,
// the request as it always was. null when the reply does not decode.
// TRLC-LINKS: REQ-SDS-023
async function rawFrame(query) {
  const r = await fetch("/api/frame.bin?" + query + frameWireQuery());
  if (!r.ok) throw new Error("http " + r.status);
  return decodeFrameReply(await r.arrayBuffer());
}

// trigSourceChan is the channel number of trigger source index src (0-based
// as the status's trig_source), or 0 for a source that is no channel.
// TRLC-LINKS: REQ-SDS-204
function trigSourceChan(src) {
  const name = (caps.trigger.sources || [])[src | 0];
  const i = caps.channels.findIndex((c) => c.name === name);
  return i >= 0 ? i + 1 : 0;
}

// trigChan is the trigger source's channel number now (1 when unknown).
// TRLC-LINKS: REQ-SDS-204
function trigChan() {
  if (!capsServed) return st && st.trig_source === 1 ? 2 : 1;
  return trigSourceChan(st ? st.trig_source : 0) || 1;
}

// frameWireQuery is the frame request's wire choice: "&v=2" when the scope
// lists frame wire v2, else nothing (v1, the request as it always was).
// TRLC-LINKS: REQ-SDS-023
function frameWireQuery() {
  return capsServed && caps.wire && Array.isArray(caps.wire.frame) && caps.wire.frame.includes(2) ? "&v=2" : "";
}

// decodeFrameReply decodes a frame reply of either wire.
// TRLC-LINKS: REQ-SDS-023
function decodeFrameReply(buf) {
  return capsServed ? decodeAnyFrame(buf) : decodeBinFrame(buf);
}

// capsControl is caps' entry for an /api/set control, or null when the
// scope does not list it (a scope without caps lists none).
// TRLC-LINKS: REQ-SDS-204
function capsControl(name) {
  return (capsServed && Array.isArray(caps.controls) && caps.controls.find((c) => c && c.name === name)) || null;
}

// feature is caps' entry for a feature: {ok, why}; a scope without caps,
// or one that does not list it, has it.
// TRLC-LINKS: REQ-SDS-204
function feature(name) {
  const f = caps.features && caps.features[name];
  return f || { ok: true };
}

// capsNormalize fills what a served document leaves out from the fallback.
// TRLC-LINKS: REQ-SDS-204
function capsNormalize(c) {
  const out = Object.assign({}, CAPS_FALLBACK, c);
  if (!Array.isArray(out.channels) || !out.channels.length) out.channels = CAPS_FALLBACK.channels;
  out.codes = Object.assign({}, CAPS_FALLBACK.codes, c.codes);
  out.trigger = Object.assign({}, CAPS_FALLBACK.trigger, c.trigger);
  if (!Array.isArray(out.trigger.sources) || !out.trigger.sources.length) out.trigger.sources = out.channels.map((ch) => ch.name);
  out.wire = Object.assign({ frame: [1] }, c.wire);
  out.features = c.features || {};
  return out;
}

// capsFollow reads the scope's caps when its status names a generation the
// page has not read (a scope without caps names none: nothing is asked).
// TRLC-LINKS: REQ-SDS-204
async function capsFollow() {
  if (!st || st.caps_gen === undefined || st.caps_gen === capsGenSeen || capsBusy) return;
  capsBusy = true;
  const gen = st.caps_gen;
  let got = null;
  try {
    const r = await fetch("/api/caps");
    if (r.ok) got = await r.json();
    else if (r.status === 404) capsGenSeen = gen; // a status that names caps the scope does not serve: the fallback stays
  } catch (e) { /* the next status poll tries again */ } finally { capsBusy = false; }
  if (!got) return;
  caps = capsNormalize(got);
  capsServed = true;
  capsGenSeen = gen;
  capsApply();
}

// CAPS_HOOKS are the page parts that follow the caps' channels themselves
// (math's operands, X-Y's pair, All settings, SOUND…): each is called after
// the channel controls are made or removed.
const CAPS_HOOKS = [];

// chanBuilt is the channels past the second whose controls the page made.
const chanBuilt = new Set();

// capsApply makes the page's channel lists, channel controls and trigger
// choices those of the caps: channels the caps add get their controls, and
// channels a new caps document no longer lists lose theirs.
// TRLC-LINKS: REQ-SDS-204
function capsApply() {
  for (const k of Object.keys(chanColorCache)) delete chanColorCache[k];
  chanStyles();
  const nums = chanNums();
  for (const n of [...chanBuilt].sort((a, b) => b - a)) if (!nums.includes(n)) chanControlsRemove(n);
  for (const n of nums) if (n > 2) chanControls(n);
  chanLayout();
  chanLists();
  trigLists();
  for (const h of CAPS_HOOKS) h();
  // the instrument's name, and why a feature the page shows is missing
  const brand = document.querySelector(".brand > span");
  if (brand && caps.identity && caps.identity.model) brand.textContent = caps.identity.model;
  const meas = feature("measure");
  for (const id of ["quickBody", "measBody"]) { const el = $(id); if (el) el.title = meas.ok ? "" : meas.why || ""; }
  if (typeof measDomSig !== "undefined") measDomSig = "";
  if (typeof applyStatus === "function" && st) applyStatus();
  if (typeof redraw === "function") redraw();
}

// chanStyles defines the cc<n> colour classes of channels past the two the
// stylesheet has.
// TRLC-LINKS: REQ-SDS-204
function chanStyles() {
  let el = document.getElementById("chanStyles");
  if (!el) { el = document.createElement("style"); el.id = "chanStyles"; document.head.appendChild(el); }
  el.textContent = chanNums().filter((n) => n > 2).map((n) =>
    `.cc${n}{color:${chanColor(n)}} button.cc${n}:not(.on){color:inherit} .seg>button.on.cc${n}{color:${chanColor(n)}}`).join("\n");
}

// CHAN_LISTS are the page's channel choosers: the element, its option
// values' base (0: the channel index, 1: the channel number) and its kind.
const CHAN_LISTS = [
  { id: "ejCh", base: 1 }, { id: "spgCh", base: 1 }, { id: "bodeRef", base: 0 }, { id: "bodeDut", base: 0 },
  { id: "pkCh", base: 1, seg: true }, { id: "measCh", base: 1, seg: true }, { id: "zmCh", base: 0, seg: true },
  { id: "srCh", base: 1 }, { id: "precisionChannel", base: 0 }, { id: "sndCh", base: 0, seg: true },
];

// chanLists gives every channel chooser one option per channel, keeping
// the options that are no channel ("both").
// TRLC-LINKS: REQ-SDS-204
function chanLists() {
  for (const l of CHAN_LISTS) setChanOptions($(l.id), l.base, !!l.seg);
  for (const sel of document.querySelectorAll(".rolesel")) setChanOptions(sel, 1, false);
}

// setChanOptions rewrites el's channel options (values base..base+N-1),
// leaving its other options where they are.
// TRLC-LINKS: REQ-SDS-204
function setChanOptions(el, base, seg) {
  if (!el) return;
  const nums = chanNums(), tag = seg ? "button" : "option";
  const isChan = (o) => /^C\d+$/.test(o.textContent.trim());
  const have = [...el.querySelectorAll(":scope > " + tag)].filter(isChan);
  if (have.length === nums.length && have.every((o, i) => o.textContent.trim() === chanName(nums[i]))) return;
  const keep = el.value;
  const after = have.length ? have[have.length - 1].nextSibling : null;
  for (const o of have) o.remove();
  for (const n of nums) {
    const o = document.createElement(tag);
    o.textContent = chanName(n);
    if (seg) { o.dataset.value = String(n - 1 + base); o.type = "button"; o.setAttribute("role", "radio"); o.className = "cc" + n; }
    else o.value = String(n - 1 + base);
    el.insertBefore(o, after);
  }
  el.value = keep;
  if (seg) mirrorRebuild(el);
}

// trigLists offers the trigger sources and types the scope has.
// TRLC-LINKS: REQ-SDS-204
function trigLists() {
  const src = $("source");
  if (src) {
    const names = caps.trigger.sources;
    const cur = [...src.querySelectorAll(":scope > button")].map((b) => b.textContent.trim());
    if (cur.join() !== names.join()) {
      const keep = src.value;
      src.innerHTML = "";
      names.forEach((name, i) => {
        const b = document.createElement("button");
        b.type = "button"; b.setAttribute("role", "radio"); b.dataset.value = String(i); b.textContent = name;
        const n = caps.channels.findIndex((c) => c.name === name) + 1;
        if (n > 0) b.className = "cc" + n;
        src.appendChild(b);
      });
      src.value = keep;
      mirrorRebuild(src);
    }
  }
  const tt = $("ttype");
  if (tt) {
    for (const b of tt.querySelectorAll(":scope > button[data-value]")) {
      const name = TRIG_TYPES[+b.dataset.value], ok = caps.trigger.types.includes(name);
      b.disabled = !ok;
      b.title = ok ? "" : "This scope has no " + name + " trigger";
    }
  }
}

// mirrorRebuild makes the mirrors of a segmented control again (their
// options are copies of the source's, taken when they were made).
// TRLC-LINKS: REQ-SDS-219
function mirrorRebuild(src) {
  if (typeof MIRRORS === "undefined") return;
  for (let i = MIRRORS.length - 1; i >= 0; i--) {
    const r = MIRRORS[i];
    if (r.src !== src || r.kind !== "seg" || !r.m.isConnected) continue;
    MIRRORS.splice(i, 1);
    const m = mirrorOf(src, { label: r.label });
    for (const c of r.m.classList) if (!m.classList.contains(c) && c !== "on") m.classList.add(c);
    r.m.replaceWith(m);
  }
}

// chanRename rewrites channel 2's names in a copied subtree for channel n:
// element ids and mirror sources (tC2, vdiv2Dn, off2v…), labels and titles
// ("CH2", "C2") and colour classes (cc2).
// TRLC-LINKS: REQ-SDS-204
function chanRename(root, n) {
  const idRe = /^(tC|vdiv|off|zero|cpl|probe|bwl|inv)2(Dn|Up|v)?$/;
  const txt = (s) => s.replace(/\bCH2\b/g, "CH" + n).replace(/\bC2\b/g, "C" + n).replace(/◂2\b/g, "◂" + n);
  const els = [root, ...root.querySelectorAll("*")];
  for (const el of els) {
    if (el.id && idRe.test(el.id)) el.id = el.id.replace(idRe, "$1" + n + "$2");
    for (const a of ["aria-label", "title"]) if (el.hasAttribute(a)) el.setAttribute(a, txt(el.getAttribute(a)));
    if (el.classList.contains("cc2")) { el.classList.remove("cc2"); el.classList.add("cc" + n); }
    if (el.dataset.src && idRe.test(el.dataset.src)) el.dataset.src = el.dataset.src.replace(idRe, "$1" + n + "$2");
    if (el.dataset.mirrorOf && idRe.test(el.dataset.mirrorOf)) {
      // a mirror is made again from its placeholder once its source exists
      const ph = document.createElement("span");
      ph.className = "mirror";
      ph.dataset.src = el.dataset.mirrorOf.replace(idRe, "$1" + n + "$2");
      if (/^tC/.test(ph.dataset.src)) ph.dataset.label = "On";
      el.replaceWith(ph);
      continue;
    }
    if (el.children.length === 0 && el.textContent) el.textContent = txt(el.textContent);
  }
}

// chanControls adds channel n's controls (n > 2), made from channel 2's:
// its group in the strip and its column in the Vertical pane, wired as
// channels 1 and 2 are.
// TRLC-LINKS: REQ-SDS-204
function chanControls(n) {
  if ($("tC" + n)) return;
  const strip = document.querySelector('.sg[aria-label="CH2"]');
  if (strip) {
    const g = strip.cloneNode(true);
    g.setAttribute("aria-label", "CH" + n);
    chanRename(g, n);
    let last = strip;
    while (last.nextElementSibling && /^CH\d+$/.test(last.nextElementSibling.getAttribute("aria-label") || "")) last = last.nextElementSibling;
    last.after(g);
  }
  const vg = document.querySelector("#p-vert .vg") || document.querySelector(".vg");
  if (vg) {
    const cols = (vg._cols = vg._cols || 3);
    const kids = [...vg.children];
    // channel 2's cell of each row, copied after the row's last
    for (let r = kids.length / cols - 1; r >= 0; r--) {
      const cell = kids[r * cols + 2].cloneNode(true);
      chanRename(cell, n);
      cell.dataset.ch = String(n);
      kids[r * cols + cols - 1].after(cell);
    }
    vg._cols = cols + 1;
  }
  chanBuilt.add(n);
  view["c" + n] = true;
  // its key shows and hides it, as 1 and 2 do channels 1 and 2
  if (n <= 9 && typeof KEYMAP !== "undefined" && !KEYMAP.some((k) => k.key === String(n))) {
    const at = KEYMAP.findIndex((k) => k.key === String(n - 1));
    // TRLC-LINKS: REQ-SDS-204
    KEYMAP.splice(at + 1, 0, { key: String(n), label: String(n), desc: "Show / hide channel " + n, run: () => $("tC" + n).click() });
  }
  for (const el of document.querySelectorAll(`[id$="${n}"].seg`)) segInit(el);
  mirrorInit();
  if (typeof wireChannel === "function") wireChannel(n);
}

// chanControlsRemove takes channel n's controls away (a caps document that
// no longer lists it): its strip group, its Vertical column, its key, its
// mirrors and its display state.
// TRLC-LINKS: REQ-SDS-204
function chanControlsRemove(n) {
  const gone = [];
  const strip = document.querySelector(`.sg[aria-label="CH${n}"]`);
  if (strip) gone.push(strip);
  const vg = document.querySelector("#p-vert .vg") || document.querySelector(".vg");
  if (vg) {
    const cells = vg.querySelectorAll(`:scope > [data-ch="${n}"]`);
    if (cells.length) { gone.push(...cells); vg._cols = (vg._cols || 3) - 1; }
  }
  // the mirrors of its controls (in All settings too) and the mirrors in it go
  // TRLC-LINKS: REQ-SDS-204
  const inGone = (el) => gone.some((g) => g.contains(el));
  if (typeof MIRRORS !== "undefined") {
    for (let i = MIRRORS.length - 1; i >= 0; i--) {
      const r = MIRRORS[i];
      if (inGone(r.src) || inGone(r.m)) { if (!inGone(r.m)) r.m.remove(); MIRRORS.splice(i, 1); }
    }
  }
  for (const el of gone) el.remove();
  if (typeof KEYMAP !== "undefined") {
    const at = KEYMAP.findIndex((k) => k.key === String(n) && /channel \d+$/.test(k.desc));
    if (at >= 0) KEYMAP.splice(at, 1);
  }
  delete view["c" + n];
  chanBuilt.delete(n);
}

// chanLayout sizes the Vertical pane's grid to its channels: the label
// column and one equal column each. Past two channels the grid is marked
// "vg-n", whose segmented controls wrap in their cell instead of overflowing
// it (design/base.css).
// TRLC-LINKS: REQ-SDS-204
function chanLayout() {
  const vg = document.querySelector("#p-vert .vg") || document.querySelector(".vg");
  if (!vg) return;
  const cols = vg._cols || 3;
  vg.style.gridTemplateColumns = cols > 3 ? "58px" + " minmax(0, 1fr)".repeat(cols - 1) : "";
  vg.classList.toggle("vg-n", cols > 3);
}

if (typeof module !== "undefined") {
  module.exports = { CAPS_FALLBACK, TRIG_TYPES, capsNormalize, setCaps: (c, served) => { caps = c; capsServed = served; } };
}
