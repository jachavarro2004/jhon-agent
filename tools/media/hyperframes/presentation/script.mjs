// tools/media/hyperframes/presentation/script.mjs — the presentation video's one script.
//
// Every shot in order: its kind, what it shows and its one narration line, `en` (what is
// written and, through `spoken`, what Kokoro says) beside `es` (its Spanish subtitle). A
// chapter's first shot opens under identity's rose band; the other shots open on a cut to
// the left. A claim shot (`head`, `card`, `list`, `numbers`) says a line in big type on the
// dark rose-lit stage; a `proof` shot holds one of Jhon's screens (a feature capture,
// cropped to its terminal panel, or a diagram) close and square-on while the camera pushes
// in and a rose mark steps through what the line names (`marks[].at`: a phrase of `en`, or
// seconds after the line starts).
//
// Every claim is README.md's (on main), docs/ldd.md's, docs/modes-and-grants.md's or a
// capture's own caption; the bench numbers are README's measured ones. `capEn`: the line
// is also shown as an English caption, because the screen does not say it.
//
// Used by narrate.mjs (speaks the lines), compose.mjs (writes index.html and
// captions.es.json) and tools/media/test/presentation.test.ts; the teaser
// (../teaser/script.mjs) takes its `spoken`, `panel`, `cells` and `lines` from here.

/** How a line is said: the mono marks dropped, names spelled as heard. */
const SAY = [
  [/--for 30m/g, "for thirty minutes"], [/delivery: change/g, "delivery change"], [/\.git\//g, "dot git"],
  [/\bLDD\b/g, "L D D"], [/\bjhon\b/gi, "John"], [/\bpi\b/g, "pie"],
  [/\bRED\b/g, "red"], [/\bGREEN\b/g, "green"], [/route-guard/g, "route guard"], [/3\.4/g, "three point four"],
];
export function spoken(text) {
  let s = text.replace(/`/g, "");
  for (const [re, to] of SAY) s = s.replace(re, to);
  return s;
}

/** A feature capture's terminal panel in its PNG (tools/media/frames.ts featureSvg at zoom 2). */
const CELL_W = 16 * 0.6154 * 2, CELL_H = 22 * 2, TEXT_X = 80 + 20 * 2, TEXT_Y = 80 + (34 + 16) * 2;
export const panel = (cols, rows) => [80, 80, (cols * 16 * 0.6154 + 40) * 2, (34 + 32 + rows * 22) * 2];
/** The rectangle of `n` cells from column `c` over `rows` rows from row `r`, in the PNG's pixels, with a margin. */
export const cells = (c, r, n, rows = 1) => [TEXT_X + c * CELL_W - 10, TEXT_Y + r * CELL_H - 3, n * CELL_W + 20, rows * CELL_H + 6];

const F = (name) => `features/${name}.png`;
const D = (name) => `diagrams/${name}.svg`;

export const SHOTS = [
  // ---------------------------------------------------------------- the intro
  { id: "intro", kind: "intro", chapter: "intro",
    en: "Jhon. Let the agent run. Deliver only what is proven.",
    es: "Jhon. Deja correr al agente. Entrega solo lo probado." },

  // ---------------------------------------------------------------- 1. Why Jhon
  { id: "problem", kind: "head", chapter: "Why Jhon",
    en: "A coding agent left alone is either asked about everything, or trusted with everything.",
    es: "Un agente de código, solo, o lo pregunta todo o se le confía todo." },
  { id: "limits", kind: "card", kicker: "Without limits", title: ["It acts *outside its task.*"],
    body: [["A destructive command.", "a destructive"], ["A write beyond its files.", "a write"]],
    en: "Trusted, it can act outside its task: a destructive command, a write beyond its files.",
    es: "Si se le confía, puede salirse de su tarea: un comando destructivo, una escritura fuera de sus archivos." },
  { id: "refusal", kind: "proof", src: F("guard-refusal"), crop: panel(112, 10),
    marks: [{ at: "every tool call", r: cells(2, 0, 51) }, { at: "outside its files", r: cells(4, 1, 97, 2) }, { at: "is refused", r: cells(0, 7, 112, 3) }],
    en: "Jhon's guards judge every tool call: a write outside its files is refused.",
    es: "Los guards de Jhon juzgan cada llamada a una herramienta: una escritura fuera de sus archivos se rechaza." },
  { id: "evidence", kind: "card", kicker: "Without evidence", title: ["It calls a change *done*"],
    body: [["with no test that failed first", "with no test"], ["and passed after.", "and passed"]],
    en: "It can call a change done, with no test that failed first and passed after.",
    es: "Puede dar un cambio por hecho sin un test que haya fallado primero y pasado después." },
  { id: "redgreen", kind: "proof", src: F("executions-red-green"), crop: [80, 80, panel(112, 33)[2], (34 + 32 + 18 * 22) * 2],
    pan: { by: 660, at: "then passes" },
    marks: [{ at: "fails first", r: cells(0, 2, 37, 2) }, { at: "then passes", delay: 0.5, r: cells(0, 21, 40) }, { at: "on record", r: cells(0, 32, 112) }],
    en: "With Jhon, the test fails first, then passes, and both runs are on record.",
    es: "Con Jhon, el test falla primero y luego pasa, y las dos ejecuciones quedan registradas." },
  { id: "memory", kind: "card", kicker: "Without memory", title: ["What was decided is *gone*"],
    body: [["with the session.", "is gone"]],
    en: "And what was decided is gone with the session.",
    es: "Y lo que se decidió se pierde con la sesión." },
  { id: "status", kind: "proof", src: F("ldd-status"), crop: panel(42, 15),
    marks: [{ at: "the change", r: cells(0, 0, 20) }, { at: "its slices", r: cells(0, 4, 42, 7) }, { at: "the next step", r: cells(0, 2, 27) }, { at: "the last test run", r: cells(0, 12, 28, 3) }],
    en: "With Jhon, the change stays in view: its slices, the next step, and the last test run.",
    es: "Con Jhon, el cambio queda a la vista: sus slices, el siguiente paso y la última ejecución de tests." },

  // ---------------------------------------------------------------- 2. Two ways in, one engine
  { id: "ways", kind: "list", chapter: "Two ways in",
    rows: [["`jhon start`", "Claude Code", "jhon start opens"], ["`jhon start pi`", "pi, with Jhon's shell", "jhon start pi"]],
    en: "There are two ways in: jhon start opens Claude Code, and jhon start pi opens pi, with Jhon's shell.",
    es: "Hay dos formas de entrar: `jhon start` abre Claude Code, y `jhon start pi` abre pi, con el shell de Jhon." },
  { id: "engine", kind: "proof", src: D("runtimes-engine"), crop: [0, 0, 1280, 720],
    marks: [{ at: 0, r: [26, 118, 1232, 194] }, { at: "one engine", r: [26, 346, 1232, 162] }, { at: "The dashboard", r: [862, 542, 396, 140] }],
    en: "Both runtimes call one engine: the jhon binary. The dashboard only reads what the binary serves.",
    es: "Los dos runtimes llaman a un solo motor: el binario `jhon`. El dashboard solo lee lo que sirve el binario." },
  { id: "bars", kind: "proof", src: F("shell-bar"), crop: panel(112, 3),
    marks: [{ at: "the project", r: cells(1, 0, 21) }, { at: "branch", r: cells(1, 2, 17) }, { at: "model", r: cells(67, 2, 22) }, { at: "context", r: cells(92, 2, 7) }, { at: "cost", r: cells(102, 2, 9) }],
    en: "In pi, the shell's bars show the project, branch, model, context, cost and jobs.",
    es: "En pi, las barras del shell muestran el proyecto, la rama, el modelo, el contexto, el costo y los trabajos." },

  // ---------------------------------------------------------------- 3. A repo with no LDD yet
  { id: "noldd", kind: "card", chapter: "A repo with no LDD yet", kicker: "A repo with no LDD yet", title: ["Read-only work *just happens.*"], body: [],
    en: "In a repo with no LDD yet, read-only work just happens.",
    es: "En un repo aún sin LDD, el trabajo de solo lectura ocurre sin más." },
  { id: "ask", kind: "proof", src: F("ldd-question"), crop: panel(84, 11),
    marks: [{ at: "asks you once", r: cells(0, 3, 44) }, { at: "how to deliver", r: cells(0, 4, 80, 2) }],
    en: "At the first code change, the session asks you once how to deliver and test.",
    es: "En el primer cambio de código, la sesión te pregunta una sola vez cómo entregar y probar." },

  // ---------------------------------------------------------------- 4. Seven guards
  { id: "seven", kind: "card", chapter: "Seven guards", kicker: "Seven guards", title: ["Every tool call, *judged.*"],
    body: [["In order, the same in both runtimes.", "in order"]],
    en: "Seven guards judge every tool call, in order, the same in both runtimes.",
    es: "Siete guards juzgan cada llamada a una herramienta, en orden, igual en los dos runtimes." },
  { id: "verdicts", kind: "proof", src: D("guards-tool-call"), crop: [0, 0, 1280, 720],
    marks: [{ at: 0, r: [262, 110, 508, 456] }, { at: "first refusal", r: [806, 110, 448, 88] }, { at: "the first ask", r: [806, 206, 448, 88] }, { at: "the call passes", r: [806, 302, 448, 88] }],
    en: "The first refusal answers. If none, the first ask. Otherwise, the call passes.",
    es: "Responde el primer rechazo. Si no hay, la primera pregunta. Si no, la llamada pasa." },

  // ---------------------------------------------------------------- 5. LDD
  { id: "ldd", kind: "card", chapter: "LDD", kicker: "LDD · Leapfrog-Driven Development", size: 92,
    title: ["Value reaches the default branch", "*in integrable, proven slices.*"], body: [],
    en: "With LDD, value reaches the default branch in integrable, proven slices.",
    es: "Con LDD, el valor llega a la rama principal en slices integrables y probados." },
  { id: "slice", kind: "proof", src: D("ldd-flow"), crop: [0, 0, 1280, 720],
    marks: [{ at: 0, r: [230, 122, 206, 198] }, { at: "a failing test", r: [434, 122, 206, 198] }, { at: "Then the tests", r: [448, 164, 176, 32] }],
    en: "Each slice: a failing test, recorded as RED. Then the code. Then the tests, green.",
    es: "Cada slice: un test que falla, registrado como RED. Luego el código. Luego los tests, en verde." },
  { id: "pr", kind: "card", kicker: "Delivery", title: ["No record, *no pull request.*"],
    body: [["RED before GREEN, and a passing verify.", "RED before"]],
    en: "The pull request opens only with RED before GREEN and a passing verify.",
    es: "El pull request solo se abre con RED antes de GREEN y un verify aprobado." },
  { id: "route", kind: "proof", src: F("route-refusal"), crop: panel(112, 9),
    marks: [{ at: "route-guard refuses", r: cells(0, 6, 112, 3) }, { at: "the pull request", r: cells(13, 0, 21) }, { at: "while a task", r: cells(4, 2, 56) }],
    en: "Under delivery: change, route-guard refuses the pull request while a task is unchecked.",
    es: "Con `delivery: change`, route-guard rechaza el pull request mientras quede una tarea sin marcar." },

  // ---------------------------------------------------------------- 6. The agents
  { id: "agents", kind: "card", chapter: "Agents", kicker: "One orchestrator · focused agents", title: ["One orchestrator *plans.*"],
    body: [["It talks with you. Focused agents do the work.", "Focused agents"]],
    en: "One orchestrator talks with you and plans. Focused agents do the work.",
    es: "Un orquestador habla contigo y planifica. Agentes enfocados hacen el trabajo." },
  { id: "launch", kind: "proof", src: D("agents-launch-context"), crop: [0, 0, 1280, 720],
    marks: [{ at: 0, r: [294, 118, 416, 352] }, { at: "the decisions", r: [308, 278, 388, 64] }, { at: "a code map", r: [308, 335, 388, 64] }],
    en: "Each launch carries the decisions on record and a code map.",
    es: "Cada lanzamiento lleva las decisiones registradas y un mapa del código." },
  { id: "jobs", kind: "proof", src: F("agents-jobs"), crop: panel(42, 8),
    marks: [{ at: 0, r: cells(0, 1, 42, 2) }, { at: "background jobs", r: cells(0, 5, 42, 3) }, { at: "tokens and time", r: cells(31, 1, 11, 2) }],
    en: "Sub-agents and background jobs show live, with their tokens and time.",
    es: "Los sub-agentes y los trabajos en segundo plano se ven en vivo, con sus tokens y su tiempo." },

  // ---------------------------------------------------------------- 7. You decide
  { id: "yours", kind: "card", chapter: "You decide", kicker: "You decide", title: ["A mode and a grant", "are *yours alone.*"], size: 100,
    body: [["An agent may read the mode, never set it.", "An agent"]],
    en: "A mode and a grant are yours alone. An agent may read the mode, never set it.",
    es: "Un modo y un grant son solo tuyos. Un agente puede leer el modo, nunca fijarlo." },
  { id: "grant", kind: "proof", src: F("mode-grant"), crop: panel(84, 8),
    marks: [{ at: 0, r: cells(0, 0, 31, 2) }, { at: "widens one class", r: cells(0, 3, 67, 2) }, { at: "for 30m", r: cells(0, 7, 71) }],
    en: "A timed grant widens one class: jhon grant push --for 30m.",
    es: "Un grant temporal amplía una clase: `jhon grant push --for 30m`." },
  { id: "hard", kind: "card", kicker: "The hard lines, in every mode", title: ["These *stay refused.*"],
    body: [["Credentials · `.git/` · the grants and modes", "Credentials"], ["Jhon's own files", "own files"]],
    en: "Credentials, .git/, the grants and modes, and Jhon's own files stay refused.",
    es: "Credenciales, `.git/`, los grants y los modos, y los archivos de Jhon siguen rechazados." },
  { id: "hardrows", kind: "proof", src: D("guards-tool-call"), crop: [0, 0, 1280, 720], capEn: true,
    marks: [{ at: 0, r: [262, 238, 508, 196] }, { at: "destructive", r: [262, 174, 508, 68] }],
    en: "So do the destructive operations no grant covers.",
    es: "También las operaciones destructivas que ningún grant cubre." },
  { id: "norepo", kind: "card", kicker: "A folder that is no repo", title: ["No repo, *no mode.*"],
    body: [["Its calls are judged as in normal mode.", "its calls"]],
    en: "A folder that is no repo has no mode: its calls are judged as in normal mode.",
    es: "Una carpeta que no es repo no tiene modo: sus llamadas se juzgan como en modo normal." },

  // ---------------------------------------------------------------- 8. Measured
  { id: "audit", kind: "card", chapter: "Measured", kicker: "Measured", title: ["`jhon audit` *reads a session.*"],
    body: [["Bypassed refusals. Untested delivery.", "It flags"], ["Edits before RED.", "edits before"]],
    en: "jhon audit reads a session afterwards, for what went wrong. It flags bypassed refusals, untested delivery and edits before RED.",
    es: "`jhon audit` lee una sesión después, buscando lo que salió mal. Señala rechazos evitados, entregas sin probar y ediciones antes de RED." },
  { id: "finding", kind: "proof", src: F("audit"), crop: panel(112, 21),
    marks: [{ at: 0, r: cells(2, 3, 41) }, { at: "a push to main", r: cells(13, 7, 36) }, { at: "no test run", r: cells(0, 5, 112, 2) }],
    en: "Here, a push to main with no test run on record.",
    es: "Aquí, un push a main sin ninguna ejecución de tests registrada." },
  { id: "bench", kind: "numbers", kicker: "The bench · native Prism review",
    figures: [["9 of 9", "seeded defects found, the same as the facet agents", "9 of 9"], ["3.4×", "fewer weighted tokens", "3.4 times"]],
    en: "On seeded defects, the native Prism review found the same 9 of 9 as the facet agents, for about 3.4 times fewer weighted tokens.",
    es: "Con defectos sembrados, la revisión Prism nativa encontró los mismos 9 de 9 que los agentes de facetas, con unas 3,4 veces menos tokens ponderados." },

  // ---------------------------------------------------------------- 9. Get started
  { id: "start", kind: "list", chapter: "Get started",
    rows: [["Install", "the one-line script", "Install"], ["Set up", "`jhon setup`", "then run"], ["Check", "`jhon doctor`", "health checks"], ["Start", "`jhon start`", "Then jhon start"]],
    en: "Install with the one-line script, then run jhon setup. jhon doctor runs the health checks. Then jhon start, inside your project.",
    es: "Instala con el script de una línea y luego ejecuta `jhon setup`. `jhon doctor` ejecuta los chequeos de salud. Luego `jhon start`, dentro de tu proyecto." },

  // ---------------------------------------------------------------- the close
  { id: "close", kind: "close", chapter: "close",
    en: "Jhon. Let the agent run. Deliver only what is proven.",
    es: "Jhon. Deja correr al agente. Entrega solo lo probado." },
];

/** The distinct spoken lines, by the first shot that says each (the close says the intro's line again). */
export function lines(shots = SHOTS) {
  const seen = new Map();
  for (const s of shots) if (!seen.has(spoken(s.en))) seen.set(spoken(s.en), s.id);
  return [...seen].map(([text, id]) => ({ id, text }));
}
