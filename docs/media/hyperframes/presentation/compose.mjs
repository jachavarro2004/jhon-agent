// docs/media/hyperframes/presentation/compose.mjs — write the presentation's composition from its script.
//
//   node docs/media/hyperframes/presentation/compose.mjs
//
// Reads script.mjs (the shots and their lines) and the narration cache (narration/<key>.json,
// each line's seconds; narrate.mjs makes it) and writes:
//   index.html        the HyperFrames composition: every shot as a timed clip with a stable id,
//                     the narration clips, the subtitle clips, the plan of times, and the two
//                     scripts it runs (timeline.js: the one paused GSAP timeline and the grain;
//                     stage.js: the intro's 3D seal), inlined
//   captions.es.json  the Spanish subtitles, one per line, as composition variables
// Every time is computed here from the lines' real lengths: a shot lasts its lead-in, its
// line and a tail (a proof is held at least PROOF_MIN seconds and its line plus PROOF_TAIL);
// a mark or a body line lands where its phrase falls in the line (by its share of the
// line's characters, which is close for Kokoro's even pace).
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { SHOTS, spoken } from "./script.mjs";
import { DIR, key } from "./narrate.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const ASSETS = join(HERE, "..", "..", "..", "assets");

const r3 = (x) => Math.round(x * 1000) / 1000;
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
const voice = (s) => JSON.parse(readFileSync(join(DIR, `${key(spoken(s.en))}.json`), "utf8"));

// ---- Timing (seconds)
const HALF = 0.28;          // a band seam: the band crosses the cut from HALF before to HALF after
const LEAD_CUT = 0.35, LEAD_BAND = 0.7, TAIL = 0.55;
const PROOF_TAIL = 1.3, PROOF_MIN = 4.8;
const INTRO = 8.0, INTRO_VO = 1.4, CLOSE_TAIL = 2.4;

// ---- Proof framing: the screen at the end of its push fits this box, above the subtitle band.
const BOX = { w: 1700, h: 790, cx: 960, cy: 462 };

function pngSize(file) {
  const b = readFileSync(file);
  return [b.readUInt32BE(16), b.readUInt32BE(20)];
}
const srcSize = (src) => (src.endsWith(".svg") ? [1280, 720] : pngSize(join(ASSETS, src)));

/** Markup: *emphasis* in the accent's italic, `code` in the mono face; split into word spans for a waterfall. */
function words(text, id) {
  const out = [];
  let em = false, code = false, buf = "";
  const flush = () => {
    for (const w of buf.split(" ").filter(Boolean)) out.push({ w, em, code });
    buf = "";
  };
  for (const ch of text) {
    if (ch === "*") { flush(); em = !em; } else if (ch === "`") { flush(); code = !code; } else if (ch === " ") { flush(); } else buf += ch;
  }
  flush();
  return out.map((x, i) => `<span class="w${x.em ? " em" : ""}${x.code ? " code" : ""}" id="${id}-w${i}">${esc(x.w)}</span>`).join(" ");
}
/** Inline markup without word spans. */
const inline = (text) => esc(text).replace(/`([^`]+)`/g, '<span class="code">$1</span>').replace(/\*([^*]+)\*/g, "<em>$1</em>");

// ---- The plan
const plan = { shots: [], seams: [], caps: [] };
let T = 0;
const timed = SHOTS.map((s, i) => {
  const v = voice(s);
  const seam = i === 0 ? "none" : s.chapter ? "band" : "cut";
  const start = T;
  const lead = seam === "band" ? LEAD_BAND : LEAD_CUT;
  let len, vo;
  if (s.kind === "intro") { len = INTRO; vo = INTRO_VO; }
  else if (s.kind === "proof") { len = lead + Math.max(v.seconds + PROOF_TAIL, PROOF_MIN); vo = start + lead; }
  else if (s.kind === "close") { len = lead + v.seconds + CLOSE_TAIL; vo = start + lead; }
  else { len = lead + v.seconds + TAIL; vo = start + lead; }
  T = start + len;
  return { s, i, seam, start, end: T, vo, sec: v.seconds, key: key(spoken(s.en)) };
});
const TOTAL = r3(T);
timed.forEach((x, i) => {
  const next = timed[i + 1];
  x.clipStart = x.seam === "band" ? x.start - HALF : x.start;
  x.clipEnd = next && next.seam === "band" ? x.end + HALF : x.end;
  x.track = 3 + (i % 2);
});

/** When a phrase of the line is said. */
function at(x, phrase, delay = 0) {
  if (typeof phrase === "number") return r3(x.vo + phrase + delay);
  const i = x.s.en.indexOf(phrase);
  if (i < 0) throw new Error(`${x.s.id}: "${phrase}" is not in its line`);
  return r3(x.vo + (x.sec * i) / x.s.en.length + delay);
}

// ---- Markup per shot
function shotHtml(x) {
  const { s } = x;
  const id = s.id;
  const open = `<div id="${id}" class="clip shot" data-start="${r3(x.clipStart)}" data-duration="${r3(x.clipEnd - x.clipStart)}" data-track-index="${x.track}" data-track-kind="graphics"><div class="rv" id="${id}-rv"><div class="cam" id="${id}-cam">`;
  const close = `</div></div></div>`;
  const p = { id, kind: s.kind, seam: x.seam, start: r3(x.start), end: r3(x.end), vo: r3(x.vo), sec: x.sec };
  let body = "";
  if (s.kind === "head") {
    body = `<div class="head-text"><div class="kick" id="${id}-kick">${esc(s.chapter)}</div>`
      + `<div class="mask"><div class="head-lead" id="${id}-lead">A coding agent left alone is either</div></div>`
      + `<div class="hl" id="${id}-l1">${words("asked about everything,", `${id}-l1`)}</div>`
      + `<div class="hl hl2" id="${id}-l2">${words("or trusted with everything.", `${id}-l2`)}</div></div>`;
    p.l1 = at(x, "asked"); p.l2 = at(x, "or trusted");
  } else if (s.kind === "card") {
    const size = s.size ?? 108;
    body = `<div class="card-pos"><div class="card" id="${id}-card"><div class="card-shadow"></div><div class="card-face">`
      + `<div class="kick card-kick" id="${id}-kick">${esc(s.kicker)}</div>`
      + `<div class="card-title" style="font-size: ${size}px">${s.title.map((t, k) => `<div class="tl">${words(t, `${id}-t${k}`)}</div>`).join("")}</div>`
      + (s.body.length ? `<div class="card-body">${s.body.map(([t], k) => `<span class="ink-line" id="${id}-b${k}">${inline(t)}</span>`).join("")}</div>` : "")
      + `</div></div></div>`;
    p.title = r3(x.vo); p.body = s.body.map(([, ph], k) => [`#${id}-b${k}`, at(x, ph)]);
  } else if (s.kind === "list") {
    body = `<div class="card-pos"><div class="card" id="${id}-card"><div class="card-shadow"></div><div class="card-face">`
      + `<div class="kick card-kick" id="${id}-kick">${esc(s.chapter)}</div><div class="list">`
      + s.rows.map(([k, v], r) => `<div class="lrow" id="${id}-r${r}"><span class="lk">${inline(k)}</span><span class="la">→</span><span class="lv">${inline(v)}</span></div>`).join("")
      + `</div></div></div></div>`;
    p.rows = s.rows.map(([, , ph], r) => [`#${id}-r${r}`, at(x, ph)]);
  } else if (s.kind === "numbers") {
    body = `<div class="card-pos"><div class="card" id="${id}-card"><div class="card-shadow"></div><div class="card-face">`
      + `<div class="kick card-kick" id="${id}-kick">${esc(s.kicker)}</div><div class="figs">`
      + s.figures.map(([n, label], k) => `<div class="fig" id="${id}-f${k}"><div class="fig-n">${esc(n)}</div><div class="fig-l">${esc(label)}</div></div>`).join("")
      + `</div></div></div></div>`;
    p.rows = s.figures.map(([, , ph], k) => [`#${id}-f${k}`, at(x, ph)]);
  } else if (s.kind === "proof") {
    const [W, H] = srcSize(s.src);
    const [cx, cy, cw, ch] = s.crop;
    const k = Math.min(BOX.w / cw, BOX.h / ch);
    const w = cw * k, h = ch * k, left = BOX.cx - w / 2, top = BOX.cy - h / 2;
    const marks = s.marks.map((m, j) => {
      const [mx, my, mw, mh] = m.r;
      return { id: `${id}-m${j}`, t: at(x, m.at, m.delay ?? 0), box: [(mx - cx) * k, (my - cy) * k, mw * k, mh * k] };
    });
    // The push heads for the first marks: its origin is their centre, in the screen's pixels.
    const focus = marks.slice(0, 2).map((m) => [m.box[0] + m.box[2] / 2, m.box[1] + m.box[3] / 2]);
    const fx = focus.reduce((a, f) => a + f[0], 0) / focus.length, fy = focus.reduce((a, f) => a + f[1], 0) / focus.length;
    body = `<div class="lit"></div><div class="screen${s.src.endsWith(".svg") ? " diagram" : ""}" id="${id}-scr" data-layout-allow-overflow style="left: ${r3(left)}px; top: ${r3(top)}px; width: ${r3(w)}px; height: ${r3(h)}px">`
      + `<div class="sheet" id="${id}-sheet"><img src="assets/screens/${s.src}" alt="" style="left: ${r3(-cx * k)}px; top: ${r3(-cy * k)}px; width: ${r3(W * k)}px; height: ${r3(H * k)}px" />`
      + marks.map((m) => `<div class="mark" id="${m.id}" style="left: ${r3(m.box[0])}px; top: ${r3(m.box[1])}px; width: ${r3(m.box[2])}px; height: ${r3(m.box[3])}px"></div>`).join("")
      + `</div></div>`;
    p.marks = marks.map((m) => [`#${m.id}`, m.t]);
    p.origin = `${r3(fx)}px ${r3(fy)}px`;
    if (s.pan) p.pan = [r3(-s.pan.by * k), at(x, s.pan.at)];
  } else if (s.kind === "close") {
    body = `<div class="close-text"><div class="close-seal"><div class="seal-shadow" id="${id}-seal-shadow"></div><div class="seal-face" id="${id}-seal-face">力</div></div>`
      + `<div class="mask" data-layout-allow-overflow><span class="pitch" id="${id}-l1">Let the agent run.</span></div>`
      + `<div class="mask" data-layout-allow-overflow><span class="pitch pitch2" id="${id}-l2">Deliver only what is <em>proven.</em></span></div>`
      + `<div class="close-url kick" id="${id}-url">github.com/jachavarro2004/jhon-agent</div></div>`;
  }
  plan.shots.push(p);
  if (s.kind === "intro") return "";
  return open + body + close;
}

const shotsHtml = timed.map(shotHtml).filter(Boolean).join("\n      ");
for (const x of timed) if (x.seam !== "none") plan.seams.push({ kind: x.seam, at: r3(x.start), out: timed[x.i - 1].s.id, inn: x.s.id });
const bands = plan.seams.filter((m) => m.kind === "band");
const bandsHtml = bands.map((m, k) => `<div id="band${k}" class="band clip" data-start="${r3(m.at - HALF - 0.02)}" data-duration="${r3(2 * HALF + 0.1)}" data-track-index="5" data-track-kind="graphics"><div class="band-in" id="band${k}-in"></div></div>`).join("\n      ");
plan.bands = bands.map((m, k) => ({ band: `#band${k}-in`, at: r3(m.at - HALF), out: m.out, inn: m.inn }));

// ---- Narration and subtitles (one per distinct shot; the close repeats the intro's line and file)
const vars = [], es = {}, caps = [], audio = [];
for (const x of timed) {
  const v = `c_${x.s.id}`;
  const en = x.s.capEn ? x.s.en.replace(/`/g, "") : "";
  vars.push({ id: v, type: "string", label: `Subtitle: ${x.s.id} (${r3(x.vo)} s)`, default: en });
  es[v] = x.s.es.replace(/`/g, ""); // subtitles are plain text: the backticks script.mjs marks commands with are dropped
  const capEnd = r3(x.vo + x.sec + 0.35);
  caps.push(`<div id="cap-${x.s.id}" class="cap clip" data-start="${r3(x.vo)}" data-duration="${r3(capEnd - x.vo)}" data-track-index="6"><span class="cap-text" id="cap-${x.s.id}-text" data-var-text="${v}">${esc(en)}</span></div>`);
  plan.caps.push([`#cap-${x.s.id}-text`, r3(x.vo), capEnd]);
  audio.push(`<audio id="vo-${x.s.id}" class="clip" src="assets/narration/${x.key}.opus" data-start="${r3(x.vo)}" data-duration="${x.sec}" data-track-index="7" data-volume="1"></audio>`);
}
plan.total = TOTAL;
plan.introEnd = INTRO;

const css = readFileSync(join(HERE, "style.css"), "utf8");
const timelineJs = readFileSync(join(HERE, "timeline.js"), "utf8");
const stageJs = readFileSync(join(HERE, "stage.js"), "utf8");
const shotList = timed.map((x) => `  ${x.s.id.padEnd(9)} ${x.s.kind.padEnd(7)} ${r3(x.start).toFixed(2).padStart(6)}-${r3(x.end).toFixed(2).padStart(6)} s  vo ${r3(x.vo).toFixed(2)} (${x.sec} s)${x.seam === "band" ? "  [band]" : ""}`).join("\n");

const html = `<!doctype html>
<!--
  Jhon — the presentation video (T63f). GENERATED by compose.mjs from script.mjs and the
  narration cache: edit those, then run \`node docs/media/hyperframes/presentation/compose.mjs\`.
  ${TOTAL} s at 1920x1080, 30 fps. Shots (start-end, where the line starts, its length):
${shotList}
-->
<html lang="en" data-composition-variables='${JSON.stringify(vars).replace(/'/g, "&#39;")}'>
  <head>
    <meta charset="utf-8" />
    <style>
${css}
    </style>
    <script src="vendor/gsap-3.15.1/gsap.min.js"></script>
    <script type="importmap">
      { "imports": { "three": "./vendor/three-0.187.0/module/three.module.js", "three/addons/": "./vendor/three-0.187.0/examples/jsm/" } }
    </script>
  </head>
  <body>
    <div id="root" data-composition-id="main" data-start="0" data-duration="${TOTAL}" data-width="1920" data-height="1080" data-fps="30">
      <canvas id="stage" width="1920" height="1080" data-layout-ignore></canvas>

      <div id="s1" class="clip" data-start="0" data-duration="${r3(INTRO + HALF)}" data-track-index="1" data-track-kind="graphics">
        <div id="s1-cam">
          <div id="s1-kick" class="kick">Agent framework · LDD</div>
          <div class="mask" data-layout-allow-overflow><span id="s1-name">Jh<em>on</em></span></div>
          <div class="mask" data-layout-allow-overflow><span class="tag" id="s1-tag1">Let the agent run.</span></div>
          <div class="mask" data-layout-allow-overflow><span class="tag" id="s1-tag2">Deliver only what is <em>proven.</em></span></div>
        </div>
      </div>

      <div id="bg" class="clip" data-start="${r3(INTRO - HALF)}" data-duration="${r3(TOTAL - INTRO + HALF)}" data-track-index="2" data-track-kind="graphics">
        <div class="stagebg" id="bg-in"><div class="pool" id="pool"></div></div>
      </div>

      ${shotsHtml}

      ${bandsHtml}

      <div id="vig" data-layout-ignore></div>
      <canvas id="grain" width="640" height="360" data-layout-ignore></canvas>

      <div id="captions" data-track-kind="captions">
        ${caps.join("\n        ")}
      </div>

      ${audio.join("\n      ")}
    </div>

    <script>
      window.__plan = ${JSON.stringify(plan)};
    </script>
    <script>
${timelineJs}
    </script>
    <script type="module">
${stageJs}
    </script>
  </body>
</html>
`;
writeFileSync(join(HERE, "index.html"), html);
writeFileSync(join(HERE, "captions.es.json"), JSON.stringify(es, null, 2) + "\n");
console.log(`${TOTAL} s, ${timed.length} shots\n${shotList}`);
