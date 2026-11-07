// ENGMODEL-OWNER-UNIT: FU-DSO-WEBCLIENT
// app_trigger.js — every trigger type a scope's instrument state holds, from
// its caps (classic script; loaded after caps.js and app_core.js): a type list
// in which the types its trigger cannot run are disabled with the reason, and
// each setting of the running type as a row, read back from the status. A
// scope whose caps carry no trigger settings (one whose engine keeps its own
// trigger) keeps the page's four-type row and qualifier panels.
"use strict";

let trigBuiltFor = ""; // the caps generation and type the rows were built for
let trigRefusedSeen = 0; // the status's refusal count last shown

// trigSend applies one control and says a refusal.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-162
async function trigSend(body) {
  try {
    const r = await (await fetch("/api/set", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })).json();
    if (!r.ok && typeof toast === "function") toast(r.err || "refused", "warn");
    if (typeof kickStatus === "function") kickStatus();
    return r;
  } catch (e) { return { ok: false }; }
}

// trigEl makes an element with a class and text.
// TRLC-LINKS: REQ-DSO-011
function trigEl(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

// TRIG_LABELS name the settings as the rows show them; a setting without one
// shows its id.
const TRIG_LABELS = {
  source: "Source", slope: "Slope", level: "Level", level_low: "Low level", limit: "Time", lower: "Lower", upper: "Upper",
  coupling: "Coupling", noise_reject: "Noise reject", holdoff: "Holdoff", timeout: "Timeout", form: "Form", count: "Edge #",
  idle: "Idle", standard: "Standard", line: "Line", field: "Field", any_line: "Any line", positive: "Positive sync",
  lines: "Lines", frame_hz: "Frame rate", interlace: "Interlace", fields: "Fields", states: "Pattern", pattern_level: "Level",
  logic: "Logic", gate_source: "Qualifier", gate_level: "Qual. level", gate_form: "Qualified by", gate_high: "Qual. high",
  slope_a: "Pattern edge", data_source: "Data", data_level: "Data level", data_high: "Data high",
};

// trigTypeLabel is a type's name as the list shows it.
// TRLC-LINKS: REQ-DSO-011
function trigTypeLabel(id) {
  if (["i2c", "spi", "uart", "can", "lin"].includes(id)) return id.toUpperCase();
  const s = id.replace(/_/g, " ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// trigInput is one setting's control.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-162
function trigInput(p, ch) {
  const send = (v) => {
    const body = { control: "trig." + p.id };
    if (typeof v === "string") body.text = v; else body.value = v;
    if (p.per_channel) body.ch = ch;
    trigSend(body);
  };
  let e;
  switch (p.kind) {
    case "choice":
    case "source": {
      e = trigEl("select");
      const opts = p.kind === "choice" ? p.choices : (caps.trigger.sources || []);
      for (const o of opts) { const op = trigEl("option", "", o.replace(/_/g, " ")); op.value = o; e.appendChild(op); }
      e.onchange = () => send(e.value);
      break;
    }
    case "bool":
      e = trigEl("input"); e.type = "checkbox";
      e.onchange = () => send(e.checked ? 1 : 0);
      break;
    case "states":
      e = trigEl("input", "w-md"); e.type = "text"; e.title = "H, L or X per input, comma separated";
      e.onchange = () => send(e.value);
      break;
    default:
      e = trigEl("input", "w-md"); e.type = "number"; e.step = p.kind === "int" ? "1" : "any";
      e.onchange = () => { const v = parseFloat(e.value); if (Number.isFinite(v)) send(v); };
  }
  e.dataset.trig = p.id;
  if (p.per_channel) e.dataset.ch = String(ch);
  e.setAttribute("aria-label", "trigger " + (TRIG_LABELS[p.id] || p.id) + (p.per_channel ? " " + chanName(ch + 1) : ""));
  return e;
}

// trigBuild lays the type list and the running type's rows out in box.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-164
function trigBuild(box, tc, type) {
  box.replaceChildren();
  const row = trigEl("div", "r"), sel = trigEl("select");
  sel.id = "trigType"; sel.setAttribute("aria-label", "trigger type");
  for (const a of tc.type_avail) {
    const op = trigEl("option", "", trigTypeLabel(a.id) + (a.ok ? "" : " (not here)"));
    op.value = a.id; op.disabled = !a.ok; if (a.why) op.title = a.why;
    sel.appendChild(op);
  }
  sel.value = type;
  sel.onchange = () => trigSend({ control: "trigtype", text: sel.value });
  row.append(trigEl("span", "l", "Type"), sel);
  box.appendChild(row);
  for (const p of (tc.params[type] || [])) {
    const r = trigEl("div", "r");
    r.appendChild(trigEl("span", "l", TRIG_LABELS[p.id] || p.id.replace(/_/g, " ")));
    if (p.per_channel) {
      for (let ch = 0; ch < caps.channels.length; ch++) { r.appendChild(trigEl("span", "hint", chanName(ch + 1))); r.appendChild(trigInput(p, ch)); }
    } else r.appendChild(trigInput(p, 0));
    if (p.unit) r.appendChild(trigEl("span", "hint", p.unit));
    box.appendChild(r);
  }
  const why = trigEl("div", "sub warn"); why.id = "trigRefused"; why.hidden = true; why.setAttribute("aria-live", "polite");
  box.appendChild(why);
}

// trigFill shows the status's settings, except in the control being edited.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-164
function trigFill(box, tr) {
  const vals = tr.params || {};
  for (const e of box.querySelectorAll("[data-trig]")) {
    if (e === document.activeElement) continue;
    let v = vals[e.dataset.trig];
    if (Array.isArray(v)) v = v[+e.dataset.ch];
    if (v === undefined) continue;
    if (e.type === "checkbox") e.checked = !!v; else e.value = String(v);
  }
  const why = box.querySelector("#trigRefused");
  if (why && tr.refused && (tr.refused_seq || 0) !== trigRefusedSeen) {
    trigRefusedSeen = tr.refused_seq || 0;
    why.textContent = "Not taken: " + tr.refused; why.hidden = false;
  }
}

// trigFollow builds and fills the trigger's rows from the caps and the
// status; on a scope whose caps carry no settings it leaves the page as is.
// TRLC-LINKS: REQ-DSO-011, REQ-DSO-164
function trigFollow() {
  const box = document.getElementById("trigAll"), tc = caps && caps.trigger;
  if (!box) return;
  if (!tc || !tc.params || !tc.type_avail) { box.hidden = true; return; }
  box.hidden = false;
  const tt = document.getElementById("ttype"), q = document.getElementById("qualrow");
  if (tt && tt.closest(".r")) tt.closest(".r").hidden = true;
  if (q) q.hidden = true;
  const tr = (typeof st !== "undefined" && st && st.trig) || {};
  if (!tr.type) return;
  const key = (caps.caps_gen || 0) + "/" + tr.type;
  if (key !== trigBuiltFor) { trigBuild(box, tc, tr.type); trigBuiltFor = key; }
  trigFill(box, tr);
}

if (typeof module !== "undefined") module.exports = { trigTypeLabel };
