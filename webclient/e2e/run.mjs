// Workflow runner: connects to the LIVE scope UI, runs the workflows whose
// `source` matches argv (the currently-flashed FPGA signal), and reports each
// one's outcome. STOPS on the first failing workflow (operator rule: a control
// that doesn't work is investigated, not retried) unless RUN_ALL=1 is set to
// collect the full failure list in one pass.
import { launch } from "./operator.mjs";
import { WORKFLOWS } from "./workflows.mjs";

const URL = process.env.SCOPE_URL || "http://192.168.1.209:8080";
const SRC = process.argv[2]; // source key filter, e.g. "tone1M"
const RUN_ALL = process.env.RUN_ALL === "1";

const list = WORKFLOWS.filter((w) => !SRC || w.source === SRC);
if (!list.length) { console.log(`no workflows for source ${SRC}`); process.exit(0); }

const op = await launch(URL);
if (!op) { console.log("SKIP: playwright not installed"); process.exit(0); }
// The browser demo (DEMO_SOURCE=1): the source is set on its generator, not
// flashed; a source it cannot make skips its workflows with the reason.
if (process.env.DEMO_SOURCE === "1" && SRC) {
  const { applySource } = await import("./demo_sources.mjs");
  const why = await applySource(op.page, SRC);
  if (why) { console.log(`SKIP: ${SRC}: ${why}`); await op.close(); process.exit(0); }
}

let pass = 0, fail = 0;
const failures = [];
for (const w of list) {
  process.stdout.write(`[${w.id}] ${w.name} ... `);
  try {
    await op.reset?.();
    await w.run(op);
    console.log("OK");
    pass++;
  } catch (e) {
    console.log("FAIL\n    → " + e.message);
    fail++;
    failures.push({ id: w.id, name: w.name, err: e.message });
    if (!RUN_ALL) break;
  }
}
await op.close();
console.log(`\n${pass}/${list.length} passed, ${fail} failed`);
if (failures.length) {
  console.log("FAILURES:");
  for (const f of failures) console.log(`  [${f.id}] ${f.name}: ${f.err}`);
  process.exit(1);
}
console.log("ALL OK");
