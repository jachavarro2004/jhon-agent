// docs/media/hyperframes/teaser/script.mjs — the teaser's one script (T141, a preview).
//
// The nine lines the owner accepted (odd/proposals/t141-plan.md, "Teaser narration", L1459),
// verbatim: `en` is spoken by Kokoro and burned in as the English caption (`capEn` on every
// shot), `es` is the Spanish subtitle. Each line has one shot, in the presentation's style and
// grammar: the same compose.mjs, narrate.mjs, style.css, timeline.js and stage.js
// (../presentation), run on this folder:
//
//   node docs/media/hyperframes/presentation/narrate.mjs "$(mktemp -d)" docs/media/hyperframes/teaser
//   node docs/media/hyperframes/presentation/compose.mjs docs/media/hyperframes/teaser
//
// Only screens that exist in docs/assets (no shot of the new shell, which does not exist yet);
// a proof's crop and marks reuse the presentation's geometry where it shows the same screen.
// Used by docs/media/test/teaser.test.ts.
import { SHOTS as FILM, cells, lines as filmLines, panel, spoken } from "../presentation/script.mjs";

export { spoken };

const F = (name) => `features/${name}.png`;
const D = (name) => `diagrams/${name}.svg`;
/** A presentation shot, for a screen both films show. */
const film = (id) => FILM.find((s) => s.id === id);

/** The install line README.md gives (its "Install" row). */
const INSTALL = "curl -fsSL https://raw.githubusercontent.com/jachavarro2004/jhon-agent/main/install.sh | sh";

export const FRAME = {
  title: "the teaser (T141, a preview)",
  // The intro says the hook, so its two lines are the hook's, not the tagline (the close says that).
  tags: ["Coding agents write fast.", "What can you *trust?*"],
  install: INSTALL,
  // Short lines, held longer than the presentation's: a proof screen is read, the install line copied.
  timing: { TAIL: 2.2, PROOF_TAIL: 2.8, PROOF_MIN: 7.5, CLOSE_TAIL: 8 },
  css: `
      /* ---- The teaser's close: the install line above the repository */
      .close-install { font-size: 26px; letter-spacing: 0; color: var(--ink); margin-bottom: 18px; }
`,
};

const shots = [
  // 1. Hook: the intro (the 3D seal, the wordmark).
  { id: "intro", kind: "intro", chapter: "intro",
    en: "Coding agents write fast. The question is what you can trust.",
    es: "Los agentes de código escriben rápido. La pregunta es en qué confiar." },

  // 2. Evidence: a card.
  { id: "evidence", kind: "card", chapter: "Evidence", kicker: "Jhon lets the agent run", size: 96,
    title: ["One thing of every change:", "*evidence.*"], body: [],
    en: "Jhon lets the agent run, and asks one thing of every change: evidence.",
    es: "Jhon deja correr al agente y le pide una sola cosa a cada cambio: evidencia." },

  // 3. RED, then GREEN: the recorded runs.
  { id: "redgreen", kind: "proof", src: film("redgreen").src, crop: film("redgreen").crop,
    pan: { by: film("redgreen").pan.by, at: "then passed" },
    marks: [{ at: "failed first", r: film("redgreen").marks[0].r }, { at: "then passed", delay: 0.5, r: film("redgreen").marks[1].r },
      { at: "Recorded", delay: 0.4, r: film("redgreen").marks[2].r }],
    en: "A test that failed first, then passed. Recorded, not claimed.",
    es: "Una prueba que primero falló y después pasó. Registrada, no declarada." },

  // 4. Only the tested tree: the LDD flow, its slice commit and its delivery.
  { id: "tree", kind: "proof", src: D("ldd-flow"), crop: [24, 112, 1232, 384],
    marks: [{ at: "the tree", r: [638, 122, 206, 198] }, { at: "is delivered", r: [1046, 122, 206, 198] }],
    en: "Only the tree that was tested is delivered.",
    es: "Solo se entrega el árbol que se probó." },

  // 5. Guards: the refused tool call, then the engine's whole reason.
  { id: "guards", kind: "proof", chapter: "Safety", src: F("guard-refusal"), crop: panel(112, 10),
    marks: [{ at: "Guards stop", r: cells(3, 3, 94) }, { at: "a write outside", r: film("refusal").marks[2].r }],
    en: "Guards stop only what can't be undone: a destructive command, a write outside the project.",
    es: "Los guards frenan solo lo que no se puede deshacer: un comando destructivo, una escritura fuera del proyecto." },

  // 6. The sandbox: a card (no capture of a sandbox run exists).
  { id: "sandbox", kind: "card", kicker: "Everything else", title: ["Runs free,", "*inside a sandbox.*"], body: [],
    en: "Everything else runs free, inside a sandbox.",
    es: "Todo lo demás corre libre, dentro de un sandbox." },

  // 7. Prism: the agents diagram's Prism review, close (its facets, the challenger, the arbiter).
  { id: "prism", kind: "proof", src: D("agents-launch-context"), crop: [724, 344, 548, 156],
    marks: [{ at: "the Prism review", r: [736, 347, 518, 143] }, { at: "four facets", r: [750, 388, 365, 40] },
      { at: "a challenger", r: [750, 432, 120, 40] }, { at: "an arbiter", r: [888, 432, 94, 40] }],
    en: "Risky changes get the Prism review: four facets, a challenger, an arbiter.",
    es: "Los cambios riesgosos pasan por la revisión Prism: cuatro facetas, un retador, un árbitro." },

  // 8. Claude Code or pi, your model, an audit of every session: jhon audit on a Claude Code session.
  { id: "audit", kind: "proof", src: F("audit"), crop: panel(112, 20),
    marks: [{ at: "Claude Code", r: cells(45, 1, 21) }, { at: "an audit", r: cells(0, 0, 94) }, { at: "every session", r: cells(0, 1, 66, 2) }],
    en: "Claude Code or pi. Your model. And an audit of every session.",
    es: "Claude Code o pi. Tu modelo. Y una auditoría de cada sesión." },

  // 9. The close: the tagline, the install line and the repository.
  { id: "close", kind: "close", chapter: "close",
    en: "Jhon. Let the agent run. Deliver only what is proven.",
    es: "Jhon. Deja correr al agente. Entrega solo lo probado." },
];

/** Every line is captioned in English too: a teaser is often watched muted. */
export const SHOTS = shots.map((s) => ({ ...s, capEn: true }));

/** The distinct spoken lines (narrate.mjs). */
export const lines = () => filmLines(SHOTS);
