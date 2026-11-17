// Jhon's dashboard: the sessions' audits, the changes and the telemetry, in
// English. It reads only what `jhon dashboard` serves
// (CONTRACT.md): /api/sessions, /api/session/<id> for one session's view,
// /api/changes for the changes page, /api/usage for the usage history,
// /api/memory/* for a project's memory, /api/integrations for Home's
// Integrations block, and /api/telemetry.
// A page `jhon audit --html` wrote carries one session in a
// <script id="session"> element instead, and shows it alone. It writes
// nothing but the findings the owner marks as reviewed, in this browser.

// ── pure helpers: tested by test/app.test.mjs ───────────────────────────────

export const esc = (s) => String(s ?? "-").replace(/[&<>"']/g, (c) => "&#" + c.charCodeAt(0) + ";");
export const tok = (n) => { n = n || 0; return n >= 1e9 ? (n / 1e9).toFixed(2) + "B" : n >= 1e6 ? (n / 1e6).toFixed(1) + "M" : n >= 1e3 ? Math.round(n / 1e3) + "K" : String(n); };
export const kb = (n) => { n = n || 0; return n >= 1e6 ? (n / 1e6).toFixed(1) + " MB" : n >= 1e3 ? Math.round(n / 1e3) + " KB" : n + " B"; };
export const pct = (x) => Math.round((x || 0) * 100) + "%";
export const dur = (ms) => { const m = Math.round((ms || 0) / 60000); return m >= 60 ? Math.floor(m / 60) + "h " + (m % 60) + "m" : m + "m"; };
export const hhmm = (iso) => iso ? new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false }) : "-";

const WEEKDAYS = ["SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"];

/** The day group a session modified at iso falls in, seen from now. */
export function dayOf(iso, now = new Date()) {
  const d = new Date(iso);
  const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((start(now) - start(d)) / 864e5);
  if (days <= 0) return "TODAY · 今日";
  if (days === 1) return "YESTERDAY · 昨日";
  if (days < 7) return "THIS WEEK · 今週";
  return "EARLIER · 以前";
}

/** The short time a session's row shows: the hour today, the weekday this week, the date before. */
export function whenOf(iso, now = new Date()) {
  const d = new Date(iso);
  const g = dayOf(iso, now);
  if (g.startsWith("TODAY") || g.startsWith("YESTERDAY")) return hhmm(iso);
  if (g.startsWith("THIS WEEK")) return WEEKDAYS[d.getDay()];
  return String(d.getDate()).padStart(2, "0") + "/" + String(d.getMonth() + 1).padStart(2, "0");
}

/** Why a change of the view needs a look, in English; empty when nothing stands out. */
export function attentionOf(c) {
  const out = [];
  const v = c.verify || {};
  if (v.fail) out.push(`verify failed ${v.fail} ${v.fail === 1 ? "time" : "times"}`);
  if (c.unfinished) out.push(`${c.unfinished} unfinished agent(s)`);
  if (c.relaunches) out.push(`${c.relaunches} relaunch(es)`);
  if (c.replans) out.push(`${c.replans} replan(s)`);
  if (c.record && !c.record.passed) out.push(c.record.tests_passed ? "the last run passed on uncommitted changes" : "the last test run failed");
  const timeouts = ((c.tests || {}).timeouts || 0) + ((c.record || {}).timeouts || 0);
  if (timeouts) out.push(`${timeouts} test(s) timed out`);
  if ((c.tests || {}).failed) out.push(`${c.tests.failed} failed test run(s)`);
  return out;
}

/**
 * A session's states in the one table (design/system.md): its glyph and word,
 * the kanji seal the dashboard sets beside them, and its colour role.
 * Needs a look is the table's ◉, its bad role as everywhere; an unreadable
 * audit is unknown (?). No glyph takes a second colour.
 */
export const STATES = {
  broken: { glyph: "✗", word: "broke a rule", seal: "破", color: "var(--broken)" },
  review: { glyph: "◉", word: "needs a look", seal: "疑", color: "var(--broken)" },
  clean: { glyph: "✓", word: "clean", seal: "済", color: "var(--ok)" },
  error: { glyph: "?", word: "unreadable", seal: "誤", color: "var(--faint)" },
  pending: { glyph: "·", word: "not audited", seal: "—", color: "var(--faint)" },
};

/** The verdict a session's view reads as: its state (glyph, word, seal, color), headline, sentence, and the tab its "Review" opens. It counts what lookItems lists, as the verdict line does. */
export function verdictOf(view) {
  const f = (view.summary || {}).findings || {};
  const broken = f.violation || 0;
  if (view.status === "audit-error") {
    return { ...STATES.error, title: "The audit could not read this session", text: "Open the notes to see what was missing or not understood in the transcript.", broken: false, go: "hallazgos" };
  }
  const items = lookItems(view).filter((i) => i.seal !== "破");
  const n = items.length;
  if (broken) {
    return { ...STATES.broken, title: broken === 1 ? "A rule was broken" : `${broken} rules were broken`,
      text: `The audit proved ${broken === 1 ? "one broken rule" : broken + " broken rules"}${n ? ` and ${n} more ${n === 1 ? "thing" : "things"} to look at` : ""}.`, broken: true, go: "hallazgos" };
  }
  if (n) {
    return { ...STATES.review, title: "Nothing broken, but something to look at",
      text: n === 1 ? `${items[0].title}.` : `${n} things to look at, first: ${items[0].title}.`, broken: false, go: items[0].tab };
  }
  if (view.status === "nothing-to-audit") {
    return { glyph: "·", word: "nothing to audit", seal: "無", color: "var(--soft)", title: "Nothing to audit", text: "The session launched no sub-agents, got no refusals and delivered nothing.", broken: false };
  }
  return { ...STATES.clean, title: "The session is clean", text: "The audit read the session and found nothing to look at.", broken: false };
}

/** The command that resumes a session in its own runtime: pi opens one by its id with --session, Claude Code with --resume. */
export function resumeCommand(meta) {
  return meta.runtime === "pi" ? `jhon start pi --session ${meta.id}` : `jhon start --resume ${meta.id}`;
}

/** What the owner should look at, most important first: each with its seal, color, headline, sentence, and the tab that explains it. */
export function lookItems(view) {
  const out = [];
  const f = (view.summary || {}).findings || {};
  if (f.violation) out.push({ seal: "破", color: "var(--broken)", title: f.violation === 1 ? "A rule was broken" : `${f.violation} rules were broken`, text: "The audit proved it from the transcript. Always look at it.", tab: "hallazgos", go: "See findings" });
  for (const c of view.changes || []) {
    const v = c.verify || {};
    const before = out.length;
    if (c.relaunches) out.push({ seal: "再", color: "var(--gold)", title: `${c.name} was relaunched ${c.relaunches} ${c.relaunches === 1 ? "time" : "times"}`, text: "An agent relaunched this often usually has a task too big or too vague.", tab: "cambios", go: "See change" });
    if (c.record && !c.record.passed && c.record.tests_passed) out.push({ seal: "未", color: "var(--gold)", title: `${c.name}'s tests passed on uncommitted changes`, text: "The last run passed, but it started with uncommitted changes: it does not test a revision that can ship. Commit and run it again.", tab: "cambios", go: "See tests" });
    else if (c.record && !c.record.passed) out.push({ seal: "赤", color: "var(--broken)", title: `${c.name}'s last test run failed`, text: (c.tests || {}).failed ? `${c.tests.failed} failed run(s); the change ended with failed tests.` : "The change ended with failed tests.", tab: "cambios", go: "See tests" });
    if (v.fail) out.push({ seal: "検", color: "var(--gold)", title: `verify failed ${v.fail} ${v.fail === 1 ? "time" : "times"} on ${c.name}`, text: "An independent verifier found a gap against the spec.", tab: "cambios", go: "See change" });
    if (c.unfinished) out.push({ seal: "止", color: "var(--gold)", title: `${c.unfinished} unfinished agent(s) in ${c.name}`, text: "They stopped before finishing: without returning their result, or on reaching their turn limit.", tab: "agentes", go: "See agents" });
    // What else makes the change need a look (timeouts, failed runs with no record): one item, so the list's look counts it too.
    const why = c.attention || attentionOf(c);
    if (out.length === before && why.length) out.push({ seal: "変", color: "var(--gold)", title: `${c.name} needs a look: ${why.join(", ")}`, text: "What the audit read of the change's runs.", tab: "cambios", go: "See change" });
  }
  if (f.suspect) out.push({ seal: "疑", color: "var(--gold)", title: `${f.suspect} finding(s) to review`, text: "A suspicion of the audit that needs human eyes.", tab: "hallazgos", go: "See findings" });
  const u = view.unattributed;
  if (u && u.share >= 0.5) out.push({ seal: "外", color: "var(--gold)", title: `${tok(u.processed)} tokens outside every change (${pct(u.share)})`, text: "The main thread and the agents with no change assigned spent most of it.", tab: "uso", go: "See usage" });
  if ((view.errors || []).length) out.push({ seal: "誤", color: "var(--faint)", title: `${view.errors.length} audit error(s)`, text: "Parts of the transcript the audit could not read.", tab: "hallazgos", go: "See notes" });
  if ((view.notes || []).length) out.push({ seal: "注", color: "var(--soft)", title: `${view.notes.length} audit note(s)`, text: "Parts of the transcript the audit could not read or chose not to count.", tab: "hallazgos", go: "See notes" });
  return out;
}

/** Where the processed tokens went: outside every change, then each change, largest first, each with its share of the total. */
export function tokenSplit(view) {
  const total = (view.summary || {}).processed || 0;
  const parts = (view.changes || []).filter((c) => c.processed).map((c) => ({ name: c.name, tokens: c.processed, share: c.share || 0 }));
  parts.sort((a, b) => b.tokens - a.tokens);
  if (view.unattributed && view.unattributed.processed) parts.unshift({ name: "Outside every change", tokens: view.unattributed.processed, share: view.unattributed.share || 0, outside: true });
  return { total, parts };
}

/** The verdict in one line (~/designs/jhon-final, 06): what is broken, then how many things «What to look at» lists below. */
export function verdictLine(view) {
  const v = verdictOf(view);
  if (view.status === "audit-error") return v.text;
  const f = (view.summary || {}).findings || {};
  const n = lookItems(view).filter((i) => i.seal !== "破").length;
  const broken = f.violation ? (f.violation === 1 ? "One broken rule" : `${f.violation} broken rules`) : "Nothing broken";
  return `${broken} · ${n ? `${n} ${n === 1 ? "thing" : "things"} to look at, below in order` : "nothing else to look at"}.`;
}

/** The timeline of the changes: the span they cover, and each change's place in it as fractions; null when no change has its times. */
export function timelineOf(view) {
  const rows = (view.changes || []).filter((c) => c.start && c.end).map((c) => ({ name: c.name, start: Date.parse(c.start), end: Date.parse(c.end), look: attentionOf(c).length > 0 }));
  if (!rows.length) return null;
  const from = Math.min(...rows.map((r) => r.start)), to = Math.max(...rows.map((r) => r.end));
  const span = Math.max(1, to - from);
  return { from, to, rows: rows.map((r) => ({ ...r, left: (r.start - from) / span, width: Math.max(0.004, (r.end - r.start) / span) })) };
}

/**
 * A listed session's state from its summary: broken, review, clean, error,
 * or pending (not audited yet). It reads the page's rule (verdictOf over
 * lookItems): a suspect, a change with something to look at, most tokens
 * outside every change, an audit error or note make it review.
 */
export function sessionState(summary) {
  if (!summary) return "pending";
  if (summary.status === "audit-error") return "error";
  if (summary.violation) return "broken";
  const outside = summary.processed ? (summary.outside || 0) / summary.processed : 0;
  if (summary.suspect || summary.look || outside >= 0.5 || summary.errors || summary.notes) return "review";
  return "clean";
}


/** A check's title as View words it (a summary's checks carry it), else its name. */
const checkTitle = (c) => c.title || c.check;

/** Why a session is in the review queue, in a few words: its broken checks, relaunches, failed tests, suspects. */
export function queueReason(sum) {
  const out = [];
  for (const c of sum.checks || []) if (c.violation) out.push(checkTitle(c).toLowerCase());
  if (sum.relaunches) out.push(`${sum.relaunches} relaunch(es)`);
  if (sum.record_failed) out.push("failed tests");
  if (sum.verify_fail) out.push(`verify failed ${sum.verify_fail} time(s)`);
  for (const c of sum.checks || []) if (!c.violation && c.suspect) out.push(`${c.suspect} ${checkTitle(c).toLowerCase()}`);
  return [...new Set(out)].join(", ") || "something to look at";
}

const RANGE_DAYS = { hoy: 1, "7d": 7, "30d": 30, todo: Infinity };

/** The Inicio page's figures over the sessions modified within the range ("hoy", "7d", "30d", "todo"). */
export function homeView(sessions, range = "7d", now = new Date()) {
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const days = RANGE_DAYS[range] ?? 7;
  const from = days === Infinity ? -Infinity : start - (days - 1) * 864e5;
  const inRange = sessions.filter((s) => Date.parse(s.modified) >= from);
  const audited = inRange.filter((s) => s.summary);
  const sum = (k) => audited.reduce((n, s) => n + (s.summary[k] || 0), 0);
  const brokenChecks = new Map();
  for (const s of audited) for (const c of s.summary.checks || []) if (c.violation) brokenChecks.set(checkTitle(c), (brokenChecks.get(checkTitle(c)) || 0) + c.violation);
  const rank = { broken: 0, review: 1 };
  const queue = audited.map((s) => ({ s, state: sessionState(s.summary) })).filter((x) => x.state === "broken" || x.state === "review")
    .sort((a, b) => rank[a.state] - rank[b.state] || Date.parse(b.s.modified) - Date.parse(a.s.modified));
  const heat = Array.from({ length: 7 }, () => Array(12).fill(0));
  for (const s of inRange) { const d = new Date(s.modified); heat[(d.getDay() + 6) % 7][Math.floor(d.getHours() / 2)]++; }
  const projects = new Set(inRange.map((s) => s.name));
  return {
    sessions: inRange.length, projects: projects.size, pending: inRange.length - audited.length,
    active_ms: sum("active_ms"), elapsed_ms: sum("elapsed_ms"), processed: sum("processed"), outside: sum("outside"),
    broken: sum("violation"), brokenChecks: [...brokenChecks].map(([title, n]) => ({ title, n })),
    queue, heat,
  };
}

/** A test run in the one vocabulary (S29, design/system.md): ✓ passed (N when counted), or ✗ N failed; null for no run. */
export function runWords(t) {
  if (!t) return null;
  if (t.passed) return { mark: "✓", text: t.count ? `passed ${t.count}` : "passed", color: "var(--ok)", ok: true };
  const failed = Array.isArray(t.failed) ? t.failed.length : t.failed || 0;
  return { mark: "✗", text: failed ? `${failed} failed` : "failed", color: "var(--broken)", ok: false };
}

const runPill = (r) => r ? `<span class="pill" style="color:${r.color}">${r.mark} ${esc(r.text)}</span>` : '<span class="muted">no run</span>';

/** Every key the page answers, as `?` lists them. */
export const KEYS = [
  ["j / k", "move down or up a list"], ["enter", "open the selected row"], ["esc", "go back, or close what is open"],
  ["g h", "go home"], ["g s", "go to the sessions"], ["g c", "go to the changes"], ["g u", "go to the usage"], ["g m", "go to the memory"], ["g t", "go to the telemetry"],
  ["ctrl k", "the command bar: find a session or a change"], ["/", "search a session; on the Memory page, search Engram"],
  ["1 … 5", "go to a session's tab"], ["r", "re-audit the session"], ["?", "show or hide the keys"],
];

const G_PAGES = { h: "#/", s: "#/sessions", c: "#/changes", u: "#/usage", m: "#/memory", t: "#/telemetry" };

/** What a key press does, given the key pressed before it (pending "g") and whether a field has the focus; pending is what the next key reads. */
export function keyAction(e, pending = "", typing = false) {
  const none = { do: null, pending: "" };
  if ((e.ctrlKey || e.metaKey) && String(e.key).toLowerCase() === "k") return { do: "bar", pending: "" };
  if (e.key === "Escape") return { do: "back", pending: "" };
  if (typing || e.ctrlKey || e.metaKey || e.altKey) return none;
  if (pending === "g") return G_PAGES[e.key] ? { do: "go", to: G_PAGES[e.key], pending: "" } : none;
  switch (e.key) {
    case "g": return { do: null, pending: "g" };
    case "j": case "ArrowDown": return { do: "move", by: 1, pending: "" };
    case "k": case "ArrowUp": return { do: "move", by: -1, pending: "" };
    case "Enter": return { do: "open", pending: "" };
    case "?": return { do: "help", pending: "" };
    case "/": return { do: "search", pending: "" };
    case "r": return { do: "reaudit", pending: "" };
  }
  if (/^[1-5]$/.test(e.key)) return { do: "tab", n: e.key, pending: "" };
  return none;
}

/** A change's address on the page: its project and its name. */
export const changeHref = (project, name) => `#/c/${encodeURIComponent(project)}/${encodeURIComponent(name)}`;

/** The command bar's answer to q: the changes, then the sessions, that hold the text; and its actions. */
export function barResults(q, sessions = [], changes = [], max = 6) {
  const t = String(q || "").trim().toLowerCase();
  const has = (...xs) => !t || xs.some((x) => String(x || "").toLowerCase().includes(t));
  const items = [
    ...changes.filter((c) => has(c.name, c.project)).slice(0, max)
      .map((c) => ({ kind: "change", label: c.name, sub: `${c.project} · ${(c.sessions || []).length} session(s)`, href: changeHref(c.project, c.name) })),
    ...sessions.filter((s) => !isScratch(s) && originOf(s) === "yours" && has(s.title, s.name, s.id, (s.summary || {}).change)).slice(0, max)
      .map((s) => ({ kind: "session", label: s.title || "Session with no request", sub: `${s.name} · ${whenOf(s.modified)}`, href: `#/s/${s.id}` })),
  ];
  const actions = [
    { label: "Open", href: items.length ? items[0].href : null },
    { label: "Sessions", href: "#/sessions" }, { label: "Changes", href: "#/changes" }, { label: "Usage", href: "#/usage" }, { label: "Memory", href: "#/memory" }, { label: "Telemetry", href: "#/telemetry" },
  ];
  return { items, actions };
}

/** A change's row on the changes page: where its slices stand, its last run, its route, and its next step; empty when its project no longer holds it. */
export function changeFacts(c) {
  const l = c.ldd;
  if (!l) return { slices: "", run: null, route: "", next: "" };
  const { states, done } = sliceStates(l);
  const at = states.indexOf("current");
  const slices = !states.length ? "" : at >= 0 ? `slice ${at + 1}/${states.length}` : `${done}/${states.length} done`;
  return { slices, run: runWords(l.tests), route: l.route || "", next: l.next || "" };
}

/** fn, called once its calls stop for ms. */
export function debounce(fn, ms) {
  let timer;
  return (...args) => { clearTimeout(timer); timer = setTimeout(() => fn(...args), ms); };
}

/** One of the server's answers: {data}, or {error} with what went wrong, so a failed read is never an empty list. */
export async function loadJSON(url, get = globalThis.fetch) {
  try {
    const r = await get(url);
    if (!r.ok) return { error: `${r.status}: ${String(await r.text()).trim()}` };
    return { data: await r.json() };
  } catch (e) {
    return { error: e && e.message ? e.message : String(e) };
  }
}

/** One row of /api/integrations as Home shows it: its name, state and reason, and its numbers in words, "—" when it has none (off, unavailable, or nothing counted). */
export function integrationLine(r) {
  const n = r && r.numbers;
  const numbers = n && typeof n.observations === "number" ? `${n.observations} observations` : "—";
  return { name: String((r && (r.display || r.name)) ?? "-"), state: String((r && r.state) || "off"), reason: String((r && r.reason) || ""), numbers };
}

/** Home's Integrations block (T58b): each integration's state, reason and numbers. Not loaded yet, or the route failing, is a line in the card, never an error panel. */
export function integrationsCard(list, error = "") {
  let body;
  if (!Array.isArray(list)) body = `<p class="muted">${error ? `could not read them: ${esc(error)}` : "Reading the integrations…"}</p>`;
  else if (!list.length) body = '<p class="muted">No integration.</p>';
  else body = `<ul class="clist">${list.map((r) => { const l = integrationLine(r); return `<li data-state="${esc(l.state)}"><b>${esc(l.name)} · ${esc(l.state)}</b><span>${l.reason ? esc(l.reason) + " · " : ""}${esc(l.numbers)}</span></li>`; }).join("")}</ul>`;
  return `<section class="card" aria-labelledby="i-h"><div class="ch"><h2 id="i-h">Integrations</h2></div><div class="cb">${body}</div></section>`;
}

/** A loading placeholder of n rows, announced as busy. */
export const skeleton = (n = 5) => `<div class="skels" aria-busy="true" aria-label="Loading">${'<div class="skel"></div>'.repeat(n)}</div>`;

// ── rendering ───────────────────────────────────────────────────────────────

const $ = (id) => document.getElementById(id);
const jp = (kanji) => `<span aria-hidden="true">${kanji}</span>`;
const meter = (value, share) => `<div class="meter">${value}<span><i style="width:${Math.min(100, Math.round((share || 0) * 100))}%"></i></span></div>`;
const table = (head, rows) => `<div class="tw"><table><thead><tr>${head.map(([h, n]) => `<th${n ? ' class="n"' : ""}>${h}</th>`).join("")}</tr></thead><tbody>${rows.join("") || `<tr><td colspan="${head.length}" class="muted">Nothing.</td></tr>`}</tbody></table></div>`;

const reviewed = {
  key: (id, where) => "jhon-dashboard:reviewed:" + id + ":" + where,
  has(id, where) { if (typeof document === "undefined") return false; try { return localStorage.getItem(this.key(id, where)) === "1"; } catch { return false; } },
  set(id, where, on) { if (typeof document === "undefined") return; try { on ? localStorage.setItem(this.key(id, where), "1") : localStorage.removeItem(this.key(id, where)); } catch { /* a private window keeps nothing */ } },
};

/** A session's triage: each finding under its check's title and explanation, as View words them (one wording, V3). */
export function findingsPanel(id, view) {
  const items = [];
  for (const g of view.findings || []) {
    for (const f of g.items) items.push({ ...f, title: g.title || g.check, explain: g.explain || "" });
  }
  items.sort((a, b) => (a.severity === b.severity ? 0 : a.severity === "violation" ? -1 : 1));
  if (!items.length) return `<div class="sh"><h2>Triage${jp("選別")}</h2><p>No findings.</p></div>`;
  return `<div class="sh"><h2>Triage${jp("選別")}</h2><p>Broken rules first. Mark each one as you review it.</p></div><div class="triage">` + items.map((f) => {
    const broken = f.severity === "violation";
    const done = reviewed.has(id, f.where);
    return `<article class="f${broken ? " hot" : ""}${done ? " done" : ""}" style="--c:var(--broken)">
      <div class="sev"><span class="jp" aria-hidden="true">${broken ? "破" : "疑"}</span><small>${broken ? "✗ broken" : "◉ to review"}</small></div>
      <div class="bd"><h3>${esc(f.title)}</h3><p>${esc(f.explain)}</p>
        ${f.evidence ? `<div class="ev">${esc(f.evidence)}</div>` : ""}
        <div class="ev" style="margin-top:8px">${esc(f.message)}</div>
        <div class="wh">${esc(f.agent)} · ${esc(f.where)}${f.kind && f.kind !== "finding" ? " · " + esc(f.kind) : ""}</div></div>
      <div class="acts"><button class="btn primary" type="button" data-review="${esc(f.where)}">${done ? "Reviewed ✓" : "Mark reviewed"}</button></div>
    </article>`;
  }).join("") + "</div>";
}

/** A change's slices from its LDD state: each done, current (the first unfinished), or pending, and how many are done. */
export function sliceStates(l) {
  let current = false;
  const states = (l.slices || []).map((sl) => {
    if (sl.total > 0 && sl.completed >= sl.total) return "done";
    if (!current) { current = true; return "current"; }
    return "pending";
  });
  return { states, done: states.filter((x) => x === "done").length };
}

const ARTIFACTS = { "exploration.md": "exploration", "research.md": "research", "proposal.md": "proposal", "specs/": "specs", "design.md": "design", "tasks.md": "tasks", "tdd-evidence.md": "TDD evidence", "verify-report.md": "verify" };

/** jhon status's notes, as the engine writes them. */
export function notesOf(l) {
  return l.notes || [];
}

/** A change's state in its project today, for the Changes pages only (L138, answer 4). */
export function lddCard(name, l) {
  const { states, done } = sliceStates(l);
  // The first slice not done, an outline one included; a payload without it names the first with an unchecked task.
  const current = l.current !== undefined ? l.current : l.slice;
  const bar = states.map((st, i) => `<i class="${st}" title="${esc(l.slices[i].name)} · ${l.slices[i].completed}/${l.slices[i].total}"></i>`).join("");
  const list = (l.slices || []).map((sl, i) => `<li class="${states[i]}"><span class="k">${states[i] === "done" ? "✓" : states[i] === "current" ? "◐" : "·"}</span><span>${esc(sl.name)}</span><span class="v">${sl.completed}/${sl.total}</span></li>`).join("");
  const arts = (l.artifacts || []).map((a) => `<span class="pill" style="color:var(--${a.state === "done" ? "ok" : "faint"})" title="${esc(a.artifact)}">${a.state === "done" ? "✓ " : ""}${esc(ARTIFACTS[a.artifact] || a.artifact)}</span>`).join("");
  const t = l.tests;
  const tests = !t ? '<span class="muted">No run recorded.</span>' :
    `${runPill(runWords(t))} <span class="muted">${whenOf(t.at)} · ${t.scope === "affected" ? "affected" : esc(t.scope || "")}${t.complete ? "" : " · incomplete"}${t.dirty ? " · with uncommitted changes" : ""}</span>${(t.failed || []).length ? `<div class="muted">Failed: ${t.failed.map((f) => `<code>${esc(f)}</code>`).join(", ")}</div>` : ""}`;
  const notes = notesOf(l).map((n) => `<li>${esc(n)}</li>`).join("");
  return `<section class="card ldd" aria-label="LDD of ${esc(name)}">
    <div class="ch"><h2>Today${jp("今日")}</h2><span class="ab"><code>${esc(name)}</code> as its project holds it today; the sessions below are its history</span></div>
    <div class="cb">
      <div class="lrow"><div class="sbar" role="img" aria-label="${done} of ${states.length} slices done">${bar}</div><b>${done} of ${states.length} slices</b><span class="muted">${l.completed}/${l.total} tasks</span></div>
      ${current ? `<p>${current === l.slice ? "In progress" : "To task"}: <b>${esc(current)}</b></p>` : states.length ? '<p style="color:var(--ok)">All slices done.</p>' : ""}
      ${l.next ? `<p>Next step: <b>${esc(l.next)}</b></p>` : ""}
      <div class="lgrid"><div><h4>Slices</h4><ul class="slices">${list || '<li class="muted">No tasks.md.</li>'}</ul></div>
      <div><h4>Last run</h4><p>${tests}</p><h4>Artifacts</h4><div class="tags">${arts}</div>${notes ? `<h4>Notes</h4><ul class="lnotes">${notes}</ul>` : ""}</div></div>
    </div></section>`;
}

/** A session's Changes tab: each change as the session worked it, with what needs a look. */
export function changesPanel(view) {
  const rows = (view.changes || []).map((c) => {
    const v = c.verify || {};
    const verify = !(v.pass || v.fail) ? '<span class="muted">-</span>' :
      (v.fail ? `<span class="pill" style="color:var(--broken)">✗ ${v.fail} failed</span>` : "") + (v.pass ? `<span class="pill" style="color:var(--ok)">✓ passed ${v.pass}</span>` : "");
    const record = !c.record ? '<span class="muted">no run</span>'
      : c.record.tests_passed && !c.record.passed ? `<span class="pill" style="color:var(--gold)" title="Passed, but started with uncommitted changes">${runWords({ passed: true }).mark} passed, uncommitted</span>`
      : runPill(runWords(c.record));
    const agents = (c.phases || []).map(([type, n]) => `<span class="pill" style="color:var(--soft)">${esc(type)} ×${esc(n)}</span>`).join("");
    const look = attentionOf(c).map((a) => `<span class="pill" style="color:var(--gold)">${esc(a)}</span>`).join("");
    return `<tr><td><code>${esc(c.name)}</code><div class="tags" style="margin-top:6px">${look}</div></td><td><div class="tags">${agents || '<span class="muted">-</span>'}</div></td><td>${verify}</td><td>${record}</td><td class="n">${dur(c.active_ms)}</td><td class="n">${meter(tok(c.processed), c.share)}</td></tr>`;
  });
  let out = `<div class="sh"><h2>Changes${jp("変更")}</h2><p>What needs a look comes first.</p></div>` +
    table([["Change"], ["Agents"], ["Verify"], ["Tests"], ["Active", 1], ["Tokens", 1]], rows);
  if (view.unattributed) out += `<p class="muted" style="margin-top:12px">Outside every change: ${esc(view.unattributed.agents)} agent(s), ${tok(view.unattributed.processed)} tokens (${pct(view.unattributed.share)}).</p>`;
  return out;
}

/** A run past this many turns is long: the audit's review mark (no executor has a turn cap). */
export const LONG_TURNS = 100;

/** Whether a sub-agent ran long: the audit says so, or it passed the review mark. */
export const isLong = (a) => Boolean(a.long) || (a.turns || 0) >= LONG_TURNS;

/** The sub-agents the filters keep, sorted: by type, by change ("*" all, "" none), by an id fragment, long ones only. */
export function filterAgents(agents, f = {}) {
  const out = (agents || []).filter((a) => (!f.type || a.type === f.type) &&
    (f.change === undefined || f.change === "*" || (f.change === "" ? !a.change : a.change === f.change)) &&
    (!f.q || String(a.id).includes(f.q)) && (!f.longOnly || isLong(a)));
  const [key, dir] = f.sort || ["processed", -1];
  return out.sort((x, y) => {
    const a = x[key] ?? "", b = y[key] ?? "";
    return (typeof a === "number" && typeof b === "number" ? a - b : String(a).localeCompare(String(b))) * dir;
  });
}

/** The agents as CSV, one row each, with a header. */
export function agentsCsv(agents) {
  const cell = (v) => { const t = String(v ?? ""); return /[",\n]/.test(t) ? `"${t.replace(/"/g, '""')}"` : t; };
  const rows = [["id", "type", "change", "phase", "turns", "peak", "tokens", "rereads", "start", "end"]];
  for (const a of agents) rows.push([a.id, a.type, a.change, a.phase, a.turns, a.peak, a.processed, a.rereads, a.start, a.end]);
  return rows.map((r) => r.map(cell).join(",")).join("\n") + "\n";
}

const agentsState = { type: "", change: "*", q: "", longOnly: false, group: true, sort: ["processed", -1], open: "" };

const AGENT_COLUMNS = [["id", "Id"], ["phase", "Phase"], ["turns", "Turns", 1], ["peak", "Peak", 1], ["processed", "Tokens", 1], ["rereads", "Rereads", 1], ["", "Note"]];

function agentRow(a, total) {
  const note = a.long ? `<span class="pill" style="color:var(--gold)" title="${esc(a.long)}">long run</span>` : isLong(a) ? '<span class="pill" style="color:var(--gold)">long run</span>' : "";
  return `<tr class="ag${agentsState.open === a.id ? " open" : ""}" data-agent="${esc(a.id)}" tabindex="0"><td><code>${esc(String(a.id).slice(0, 10))}</code></td><td>${esc(a.phase || "-")}</td><td class="n">${a.turns}</td><td class="n">${tok(a.peak)}</td><td class="n">${meter(tok(a.processed), total ? a.processed / total : 0)}</td><td class="n">${a.rereads || 0}</td><td>${note}</td></tr>`;
}

function agentDrawer(a, view) {
  if (!a) return "";
  const total = (view.summary || {}).processed || 0;
  const span = a.start && a.end ? `${hhmm(a.start)} → ${hhmm(a.end)} · ${dur(Date.parse(a.end) - Date.parse(a.start))}` : "";
  const r = a.reads || {};
  const reads = READS.filter(([k]) => r[k]).sort((x, y) => r[y[0]] - r[x[0]]).map(([k, label]) => `<li>${label}<span class="v">${kb(r[k])}</span></li>`).join("");
  const rereads = (a.reread_files || []).map((f) => `<li><code>${esc(f.path)}</code><span class="v">×${f.count}</span></li>`).join("");
  const why = a.long ? `Long run: ${esc(a.long)}.` : isLong(a) ? `Long run: ${a.turns} turns, past the review mark of ${LONG_TURNS}.` : "";
  return `<aside class="drawer" aria-label="Agent detail">
    <div class="dw-h"><div><div class="kick">${esc(a.type)}${a.phase ? ` · ${esc(a.phase)}` : ""} · 隊</div><h3>${esc(String(a.id).slice(0, 10))}</h3><div class="muted">${a.change ? `<code>${esc(a.change)}</code>` : "outside changes"}${span ? ` · ${span}` : ""}</div></div><button class="tb" type="button" data-close-drawer aria-label="Close">×</button></div>
    ${why ? `<div class="tip">${why}</div>` : ""}
    <div class="dw-n"><div><small>Tokens</small><b>${tok(a.processed)}</b></div><div><small>Of total</small><b>${pct(total ? a.processed / total : 0)}</b></div><div><small>Turns</small><b>${a.turns}</b></div><div><small>Peak</small><b>${tok(a.peak)}</b></div></div>
    <h4>What it read</h4><ul class="leg">${reads || '<li class="muted">Nothing.</li>'}</ul>
    <h4>Files it reread</h4><ul class="leg">${rereads || '<li class="muted">None.</li>'}</ul>
    <div class="dw-a"><button class="tb" type="button" data-copy-id="${esc(a.id)}">Copy id</button><button class="tb" type="button" data-go="resumen">See on the timeline</button></div>
  </aside>`;
}

function agentsPanel(view) {
  const st = agentsState;
  const all = view.agents || [];
  const total = (view.summary || {}).processed || 0;
  const cards = (view.agentTypes || []).map((t) => `<button type="button" class="tcard${st.type === t.type ? " on" : ""}" data-type="${esc(t.type)}"><b>${esc(t.type)}</b><span class="big2">${tok(t.processed)}</span><span class="muted">${t.count} run(s) · ${t.avg_turns} avg turns · peak ${tok(t.peak)}</span>${t.rereads ? `<span class="warn">⚠ ${t.rereads} rereads of the same file</span>` : ""}</button>`).join("");
  const changes = [...new Set(all.map((a) => a.change).filter(Boolean))];
  const chips = [["*", "all"], ...changes.map((c) => [c, c]), ["", "outside changes"]].map(([v, label]) => `<button type="button" data-change="${esc(v)}" aria-pressed="${st.change === v}">${esc(label)}</button>`).join("");
  const list = filterAgents(all, st);
  const head = `<thead><tr>${AGENT_COLUMNS.map(([k, label, n]) => `<th${n ? ' class="n"' : ""}>${k ? `<button type="button" class="sort" data-sort="${k}">${label}${st.sort[0] === k ? (st.sort[1] < 0 ? " ↓" : " ↑") : ""}</button>` : label}</th>`).join("")}</tr></thead>`;
  let body = "";
  if (st.group) {
    const groups = new Map();
    for (const a of list) { const k = a.change || ""; if (!groups.has(k)) groups.set(k, []); groups.get(k).push(a); }
    for (const [k, rows] of groups) {
      body += `<tr class="grp"><td colspan="7">▾ ${k ? `<code>${esc(k)}</code>` : "outside changes"} · ${rows.length} agent(s) · ${tok(rows.reduce((n, a) => n + (a.processed || 0), 0))}</td></tr>` + rows.map((a) => agentRow(a, total)).join("");
    }
  } else body = list.map((a) => agentRow(a, total)).join("");
  if (!list.length) body = `<tr><td colspan="7" class="muted">No agent matches the filters.</td></tr>`;
  const open = all.find((a) => a.id === st.open);
  return `<div class="tcards">${cards}</div>
    <div class="afilters"><span class="chips" role="group" aria-label="Change">${chips}</span>
      <label class="search sm"><input type="search" data-agent-q placeholder="Search id" value="${esc(st.q)}" aria-label="Search an agent by id"></label>
      <label class="chk"><input type="checkbox" data-long${st.longOnly ? " checked" : ""}> Long runs only</label>
      <label class="chk"><input type="checkbox" data-group${st.group ? " checked" : ""}> Group by change</label>
      <button class="tb" type="button" data-csv>Export CSV</button>
      <span class="muted">${list.length} of ${all.length}${st.type ? ` ${esc(st.type)}` : ""}</span></div>
    <div class="agrid${open ? " with-drawer" : ""}"><div class="tw"><table class="atable">${head}<tbody>${body}</tbody></table></div>${agentDrawer(open, view)}</div>`;
}

/** Re-renders the Agentes tab in place and wires its controls. */
function bindAgents(view, select) {
  const panel = $("p-agentes");
  if (!panel) return;
  const redraw = (focus) => {
    panel.innerHTML = agentsPanel(view);
    bindAgents(view, select);
    if (focus) { const el = panel.querySelector(focus); if (el) { el.focus(); if (el.setSelectionRange) el.setSelectionRange(el.value.length, el.value.length); } }
  };
  for (const b of panel.querySelectorAll("[data-type]")) b.addEventListener("click", () => { agentsState.type = agentsState.type === b.dataset.type ? "" : b.dataset.type; redraw(); });
  for (const b of panel.querySelectorAll("[data-change]")) b.addEventListener("click", () => { agentsState.change = b.dataset.change; redraw(); });
  for (const b of panel.querySelectorAll("[data-sort]")) b.addEventListener("click", () => { const k = b.dataset.sort; agentsState.sort = [k, agentsState.sort[0] === k ? -agentsState.sort[1] : -1]; redraw(); });
  const q = panel.querySelector("[data-agent-q]");
  if (q) q.addEventListener("input", () => { agentsState.q = q.value; redraw("[data-agent-q]"); });
  const long = panel.querySelector("[data-long]");
  if (long) long.addEventListener("change", () => { agentsState.longOnly = long.checked; redraw(); });
  const group = panel.querySelector("[data-group]");
  if (group) group.addEventListener("change", () => { agentsState.group = group.checked; redraw(); });
  const csv = panel.querySelector("[data-csv]");
  if (csv) csv.addEventListener("click", () => {
    const blob = new Blob([agentsCsv(filterAgents(view.agents, agentsState))], { type: "text/csv" });
    const a = Object.assign(document.createElement("a"), { href: URL.createObjectURL(blob), download: "jhon-agents.csv" });
    a.click();
    URL.revokeObjectURL(a.href);
  });
  for (const tr of panel.querySelectorAll("[data-agent]")) {
    const openIt = () => { agentsState.open = agentsState.open === tr.dataset.agent ? "" : tr.dataset.agent; redraw(); };
    tr.addEventListener("click", openIt);
    tr.addEventListener("keydown", (e) => { if (e.key === "Enter") openIt(); });
  }
  const close = panel.querySelector("[data-close-drawer]");
  if (close) close.addEventListener("click", () => { agentsState.open = ""; redraw(); });
  const copy = panel.querySelector("[data-copy-id]");
  if (copy) copy.addEventListener("click", async () => { try { await navigator.clipboard.writeText(copy.dataset.copyId); copy.textContent = "Copied ✓"; } catch { window.prompt("Copy the id:", copy.dataset.copyId); } });
  for (const a of panel.querySelectorAll("[data-go]")) a.addEventListener("click", (e) => { e.preventDefault(); select($("tab-" + a.dataset.go)); });
}

const READS = [["full", "Whole files"], ["ranged", "File ranges"], ["shell", "Shell output"], ["codegraph", "CodeGraph"], ["search", "Searches"], ["engram", "Engram"], ["other", "Other"]];

function readingPanel(view) {
  const r = view.reads || {};
  const total = READS.reduce((n, [k]) => n + (r[k] || 0), 0) || 1;
  const rows = READS.filter(([k]) => r[k]).sort((a, b) => r[b[0]] - r[a[0]]).map(([k, label]) => `<tr><td>${label}</td><td class="n">${meter(kb(r[k]), r[k] / total)}</td></tr>`);
  return `<div class="sh"><h2>What the agents read${jp("読込")}</h2><p>Bytes of tool output by source. Everything read is read again on every later turn.</p></div>` + table([["Source"], ["Bytes", 1]], rows);
}

/** The owner's yeses to a guard's question, one line each: the classes, the scope, when. Empty when none. */
export function authorizationLines(view, now = new Date()) {
  return (view.authorizations || []).map((a) => `${(a.classes || []).join(" and ")} · ${a.scope === "grant" ? "allowed for 30 min" : "this once only"} · ${whenOf(a.at, now)}${a.ask ? ` · ${a.ask}` : ""}`);
}

function notesPanel(view) {
  const yes = authorizationLines(view);
  const allowed = yes.length ? `<section><div class="sh"><h2>What you allowed${jp("許可")}</h2><p>Your yes answers to the safety guard in this session.</p></div><ul class="notes">${yes.map((l) => `<li>${esc(l)}</li>`).join("")}</ul></section>` : "";
  const list = (xs) => xs.length ? `<ul class="notes">${xs.map((x) => `<li>${esc(typeof x === "string" ? x : `${x.file || ""}${x.line ? ":" + x.line : ""} ${x.message || JSON.stringify(x)}`)}</li>`).join("")}</ul>` : '<p class="muted">Nothing.</p>';
  return allowed + `<section><div class="sh"><h2>Audit errors${jp("誤り")}</h2></div>${list(view.errors || [])}</section><section><div class="sh"><h2>Audit notes${jp("注記")}</h2><p>What the audit noted while reading the session.</p></div>${list(view.notes || [])}</section>`;
}

const TABS = [
  ["resumen", "概", "Summary", () => ""],
  ["cambios", "変", "Changes", (v) => v.summary.changes],
  ["agentes", "隊", "Agents", (v) => v.summary.subagents],
  // Hallazgos holds what the audit read and noted too (Lectura and Notas).
  ["hallazgos", "指", "Findings", (v) => { const f = v.summary.findings; const n = f.violation + f.suspect; const notes = (v.errors || []).length + (v.notes || []).length; return n || (notes ? `${notes} notes` : ""); }],
  ["uso", "量", "Usage", () => ""],
];

/** The token split's parts: the rose ladder of the palette's roles, no hue outside it (design/system.md). */
const SPLIT_COLORS = ["var(--rosa)", "var(--accent-soft)", "var(--frame-soft)", "var(--rosa-fill)", "var(--soft)", "var(--faint)"];

// Outside a browser (the tests import this module) there is no storage to
// read, and Node warns on touching it.
const browser = typeof document !== "undefined";

const pref = {
  get(key, fallback) { if (!browser) return fallback; try { return localStorage.getItem("jhon-dashboard:" + key) ?? fallback; } catch { return fallback; } },
  set(key, value) { if (!browser) return; try { localStorage.setItem("jhon-dashboard:" + key, value); } catch { /* a private window keeps nothing */ } },
};

const WORKED = { delivery: "Delivery", tests: "Tests", verify: "Verify", agents: "Agents", records: "Records", guards: "Guards" };

/** What the session did right, as the audit read it: what, where, and the evidence. Empty when nothing. */
export function workedItems(view) {
  return (view.worked || []).map((w) => ({
    title: `${WORKED[w.kind] || w.kind}${w.change ? ` · ${w.change}` : ""}: ${w.what}`,
    text: w.evidence,
  }));
}

const secs = (ms) => ms >= 60000 ? `${(ms / 60000).toFixed(1).replace(/\.0$/, "")}m` : `${Math.round(ms / 1000)}s`;
const median = (xs) => { const s = [...xs].sort((a, b) => a - b); return s.length ? s[Math.floor((s.length - 1) / 2)] : 0; };

/** How the session ran, from the audit's timing: the main thread, the sub-agents, the compactions. Empty when not measured. */
export function timingRows(view) {
  const t = view.timing;
  if (!t || !t.main) return [];
  const pooled = (xs) => { const p = xs.reduce((n, a) => n + a.processed, 0); return { cache_hit: p ? xs.reduce((n, a) => n + a.cache_read, 0) / p : null }; };
  const cache = (x) => x.cache_hit == null ? "" : ` · cache ${Math.round(x.cache_hit * 100)}%`;
  const m = t.main;
  const rows = [{ label: "Main thread", value: m.turns ? `${secs(m.model_ms / m.turns)} per turn` : "no turns",
    note: `${m.turns} turn(s) · model ${secs(m.model_ms)} · tools ${secs(m.tool_ms)}${cache(m)}` }];
  const agents = t.agents || [];
  if (agents.length) {
    const firsts = agents.map((a) => a.first_response_ms).filter((x) => x != null);
    const slow = agents.reduce((a, b) => (b.first_response_ms ?? 0) > (a.first_response_ms ?? 0) ? b : a);
    const turns = agents.reduce((n, a) => n + a.turns, 0);
    const model = agents.reduce((n, a) => n + a.model_ms, 0);
    rows.push({ label: "Sub-agent start-up", value: firsts.length ? `${secs(median(firsts))} to a first response` : "not measured",
      note: `median of ${agents.length}${firsts.length ? ` · slowest ${secs(slow.first_response_ms)} (${slow.agent_type})` : ""}` });
    rows.push({ label: "Sub-agents", value: turns ? `${secs(model / turns)} per turn` : "no turns",
      note: `${turns} turn(s) · model ${secs(model)} · tools ${secs(agents.reduce((n, a) => n + a.tool_ms, 0))}${cache(pooled(agents))}` });
  }
  const c = t.compactions || [];
  rows.push({ label: "Compactions", value: c.length ? String(c.length) : "none",
    note: c.length ? c.map((x) => `${hhmm(x.at)} ${Math.round(x.tokens_before / 1000)}k${x.trigger ? ` ${x.trigger}` : ""}${x.duration_ms ? ` · ${secs(x.duration_ms)}` : ""}`).join(" · ") : "no compaction in this session" });
  return rows;
}

/** A session's Summary tab: what to look at, what worked, how it ran, where the tokens went, and when each change ran. */
export function summaryPanel(view) {
  const items = lookItems(view);
  const look = items.length
    ? `<ul class="att">${items.map((i) => `<li style="--c:${i.color}"><span class="k" aria-hidden="true">${i.seal}</span><div><b>${esc(i.title)}</b><p>${esc(i.text)}</p></div><a class="go" href="#" data-go="${i.tab}">${i.go} →</a></li>`).join("")}</ul>`
    : '<p class="muted">Nothing to look at: the audit found nothing that needs your attention.</p>';
  const done = workedItems(view);
  const worked = done.length
    ? `<section class="card" aria-labelledby="w-h"><div class="ch"><h2 id="w-h">What worked${jp("成果")}</h2><span class="ab">with its evidence</span></div><div class="cb"><ul class="att">${done.map((i) => `<li style="--c:var(--ok)"><span class="k" aria-hidden="true">✓</span><div><b>${esc(i.title)}</b><p>${esc(i.text)}</p></div></li>`).join("")}</ul></div></section>`
    : "";
  const ran = timingRows(view);
  const timing = ran.length
    ? `<section class="card" aria-labelledby="r-h"><div class="ch"><h2 id="r-h">How it ran${jp("速度")}</h2><span class="ab">where the waiting went</span></div><div class="cb">${table([["Part"], ["", 1]], ran.map((r) => `<tr><td>${esc(r.label)}<div class="muted">${esc(r.note)}</div></td><td class="n"><b>${esc(r.value)}</b></td></tr>`))}</div></section>`
    : "";
  const split = tokenSplit(view);
  const color = (p, i) => p.outside ? "var(--surface-hi)" : SPLIT_COLORS[i % SPLIT_COLORS.length];
  let ci = 0;
  const painted = split.parts.map((p) => ({ ...p, color: color(p, p.outside ? 0 : ci++) }));
  const outside = split.parts.find((p) => p.outside);
  const tip = outside && outside.share >= 0.5 && (view.summary || {}).resumes
    ? `<div class="tip">Every turn rereads its whole context. With ${view.summary.resumes} resume(s), the main thread dragged a lot of context along: splitting the session would have cost less.</div>` : "";
  const tokens = `<div class="big">${tok(split.total)}<small>tokens processed</small></div>
    <div class="stack" role="img" aria-label="${esc(painted.map((p) => `${p.name} ${pct(p.share)}`).join(", "))}">${painted.map((p) => `<i style="width:${(p.share * 100).toFixed(2)}%;background:${p.color}"></i>`).join("")}</div>
    <ul class="leg">${painted.map((p) => `<li><i style="background:${p.color}"></i>${esc(p.name)}<span class="v">${tok(p.tokens)} · ${pct(p.share)}</span></li>`).join("")}</ul>${tip}`;
  const tl = timelineOf(view);
  const timeline = tl ? (() => {
    const ticks = Array.from({ length: 6 }, (_, i) => `<span>${hhmm(new Date(tl.from + (tl.to - tl.from) * i / 5).toISOString())}</span>`).join("");
    const rows = tl.rows.map((r) => `<span class="lab"${r.look ? ' style="color:var(--gold)"' : ""}>${esc(r.name)}</span><div class="track" role="img" aria-label="${esc(r.name)}: from ${hhmm(new Date(r.start).toISOString())} to ${hhmm(new Date(r.end).toISOString())}"><i style="left:${(r.left * 100).toFixed(2)}%;width:${(r.width * 100).toFixed(2)}%;background:${r.look ? "var(--gold)" : "var(--rosa)"}"></i></div>`).join("");
    return `<div class="tl"><span></span><div class="ticks">${ticks}</div>${rows}</div><div class="legend2"><span><i style="background:var(--rosa)"></i>change</span><span><i style="background:var(--gold)"></i>change with something to look at</span></div>`;
  })() : '<p class="muted">No change carries its start and end times.</p>';
  return `<div class="cards">
    <section class="card${items.length ? " hot" : ""}" aria-labelledby="a-h"><div class="ch"><h2 id="a-h">What to look at${jp("注目")}</h2><span class="ab">most important first</span></div><div class="cb">${look}</div></section>
    ${worked}
    ${timing}
    <section class="card" aria-labelledby="t-h"><div class="ch"><h2 id="t-h">Where the tokens went${jp("消費")}</h2><a href="#" data-go="uso">Usage →</a></div><div class="cb">${tokens}</div></section>
    <section class="card span2" aria-labelledby="l-h"><div class="ch"><h2 id="l-h">Timeline${jp("流れ")}</h2><span class="ab">When each change ran, from start to end.</span></div><div class="cb">${timeline}</div></section>
    </div>`;
}

/**
 * What a range covers, in words, from what the server says it listed
 * ({listed, total, capped}): "all history" only when no session was cut,
 * else the N most recent sessions (T17d, L142).
 */
export function coverageLabel(range, cov) {
  const cut = cov && cov.capped ? `the ${cov.listed} most recent sessions` : "";
  if (range === "todo") return cut || "all history";
  const days = { hoy: "today", "7d": "the last 7 days", "30d": "the last 30 days" }[range] || "the last 7 days";
  return cut ? `${days} (of ${cut})` : days;
}

/** The Memory picker's note: which projects it can offer when the list is cut. */
export const memoryPickerNote = (cov) => (cov && cov.capped ? `Projects of the ${cov.listed} most recent sessions.` : "");

/** Dollars as pi recorded them, "—" where nothing was; a sum names what it covers ("pi only"). Never an estimate (L138, answer 3). */
export function money(cost, covers) {
  if (cost == null) return "—";
  return `$${cost.toFixed(2)}${covers === "pi only" ? " · pi only" : ""}`;
}

/** A local day as YYYY-MM-DD. */
const dayKey = (d) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

/** A YYYY-MM-DD day as the page reads it: "3 Nov". */
export const monthDay = (day) => new Date(`${day}T00:00`).toLocaleDateString("en-GB", { day: "numeric", month: "short" });

/** The Usage page's question: Home's range ("hoy", "7d", "30d", "todo") as days, and the Sessions page's project ("" for all). */
export function usageQuery(range, project = "", now = new Date()) {
  const days = RANGE_DAYS[range] ?? 7;
  const from = days === Infinity ? "" : dayKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - (days - 1)));
  return `/api/usage?${from ? `from=${from}&` : ""}to=${dayKey(now)}${project ? `&project=${encodeURIComponent(project)}` : ""}`;
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;

/** The Usage page's figures from /api/usage: tokens, money, active days and streaks, the most active day, the longest session, the favourite model. */
export function usageFigures(u) {
  const t = u.totals || {};
  const split = `${tok(t.input)} in · ${tok(t.output)} out · ${tok(t.cache_read)} cache read · ${tok(t.cache_write)} cache write`;
  const l = u.longest_session;
  return [
    { label: "Tokens", value: tok(t.total), note: `in ${u.sessions} session(s)${u.pending ? ` · ${u.pending} not audited yet` : ""}` },
    { label: "Money", value: money(t.cost, u.cost_covers), note: t.cost == null ? "Claude Code records no money; pi recorded none here" : "recorded by pi; Claude Code records none" },
    { label: "Active days", value: `${u.active_days}/${u.days_in_range}`, note: `longest streak ${plural(u.longest_streak, "day", "days")} · current ${plural(u.current_streak, "day", "days")}` },
    { label: "Most active day", value: u.most_active_day ? monthDay(u.most_active_day.day) : "—", note: u.most_active_day ? `${tok(u.most_active_day.total)} tokens` : "no usage in this range" },
    l ? { label: "Longest session", value: dur(l.active_ms), note: `${l.title || "Session with no request"} · ${l.project}`, href: `#/s/${l.id}` }
      : { label: "Longest session", value: "—", note: "no session in this range" },
    { label: "Favourite model", value: (u.models || [])[0] ? u.models[0].model : "—", note: t.total ? split : "no model answered in this range" },
  ];
}

/** One session's usage from its rows (/api/session's usage): its models, most tokens first, the one it used most, its totals, and what its money covers. */
export function sessionUsage(usage) {
  const totals = { input: 0, output: 0, cache_read: 0, cache_write: 0, total: 0, cost: null, messages: 0 };
  const models = new Map();
  let priced = false, unpriced = false;
  for (const r of (usage || {}).rows || []) {
    for (const k of ["input", "output", "cache_read", "cache_write", "total", "messages"]) totals[k] += r[k] || 0;
    const m = models.get(r.model) || { model: r.model, total: 0, cost: null, messages: 0 };
    m.total += r.total || 0;
    m.messages += r.messages || 0;
    if (r.cost != null) { m.cost = (m.cost || 0) + r.cost; totals.cost = (totals.cost || 0) + r.cost; priced = true; } else unpriced = true;
    models.set(r.model, m);
  }
  const list = [...models.values()].sort((a, b) => b.total - a.total || b.messages - a.messages || a.model.localeCompare(b.model));
  return { model: list.length ? list[0].model : null, models: list, totals, covers: priced && unpriced ? "pi only" : priced ? "all" : "none" };
}

/** A session's Usage tab: its model, money, messages and token breakdown, then who spent the tokens, by change and by agent type. */
function usagePanel(view, usage) {
  const u = sessionUsage(usage);
  const t = u.totals;
  const fields = [["Model", u.model ? esc(u.model) : "—"], ["Money", esc(money(t.cost, u.covers))], ["Messages", String(t.messages)],
    ["Tokens", `${tok(t.total)} · ${tok(t.input)} in · ${tok(t.output)} out · ${tok(t.cache_read)} cache read · ${tok(t.cache_write)} cache write`]];
  const recorded = `<section><div class="sh"><h2>Usage${jp("量")}</h2><p>What the session's transcripts record of the model's usage, its sub-agents' included. Money is what pi recorded; Claude Code records none.</p></div>
    ${u.models.length ? table([["What"], ["Recorded", 1]], fields.map(([k, v]) => `<tr><td>${k}</td><td class="n">${v}</td></tr>`)) : '<p class="muted">The transcript records no usage.</p>'}
    ${u.models.length > 1 ? table([["Model"], ["Tokens", 1], ["Money", 1], ["Messages", 1]], u.models.map((m) => `<tr><td><code>${esc(m.model)}</code></td><td class="n">${meter(tok(m.total), t.total ? m.total / t.total : 0)}</td><td class="n">${esc(money(m.cost))}</td><td class="n">${m.messages}</td></tr>`)) : ""}</section>`;
  return recorded + spentPanel(view);
}

function spentPanel(view) {
  const s = view.summary || {};
  const total = s.processed || 0;
  const agentsTotal = (view.agents || []).reduce((n, a) => n + (a.processed || 0), 0);
  const main = Math.max(0, total - agentsTotal);
  const who = [["Main thread", main], ["Sub-agents", agentsTotal]].map(([label, n]) => `<tr><td>${label}</td><td class="n">${meter(tok(n), total ? n / total : 0)}</td></tr>`);
  const split = tokenSplit(view).parts.map((p) => `<tr><td>${p.outside ? esc(p.name) : `<code>${esc(p.name)}</code>`}</td><td class="n">${meter(tok(p.tokens), p.share)}</td></tr>`);
  const types = (view.agentTypes || []).map((t) => `<tr><td><code>${esc(t.type)}</code></td><td class="n">${t.count}</td><td class="n">${tok(t.peak)}</td><td class="n">${meter(tok(t.processed), t.share)}</td></tr>`);
  return `<section><div class="sh"><h2>Who spent${jp("誰")}</h2><p>${tok(total)} tokens processed in ${dur(s.active_ms)} active.</p></div>${table([["Part"], ["Tokens", 1]], who)}</section>
    <section><div class="sh"><h2>By change${jp("変更")}</h2><p>What has no change assigned is shown apart.</p></div>${table([["Change"], ["Tokens", 1]], split)}</section>
    <section><div class="sh"><h2>By agent type${jp("種類")}</h2><p>The peak is the largest context of a single turn.</p></div>${table([["Type"], ["Runs", 1], ["Peak", 1], ["Tokens", 1]], types)}</section>`;
}

/** The keys a page answers: all of them served, a session's own in the page jhon audit --html writes. */
const keysOf = (embedded) => embedded ? KEYS.filter(([k]) => ["1 … 5", "?", "esc"].includes(k)) : KEYS;

/** The keys' list, shown and hidden by ? (one for the whole page). */
function toggleKeys(embedded, show) {
  let el = $("keys");
  if (!el) {
    el = Object.assign(document.createElement("div"), { id: "keys", className: "help keys", hidden: true });
    el.setAttribute("role", "dialog");
    el.setAttribute("aria-label", "Keys");
    el.innerHTML = `<b>Keys</b><ul>${keysOf(embedded).map(([k, d]) => `<li><kbd>${esc(k)}</kbd> ${esc(d)}</li>`).join("")}</ul>`;
    document.body.append(el);
  }
  el.hidden = show === undefined ? !el.hidden : !show;
}

function renderSession(meta, view, tab = "resumen", options = {}) {
  if (agentsState.session !== meta.id) Object.assign(agentsState, { type: "", change: "*", q: "", longOnly: false, open: "", session: meta.id });
  const v = verdictOf(view);
  const s = view.summary || {};
  const f = s.findings || {};
  const kind = view.session || {};
  const id = String(meta.id || "");
  const look = (view.changes || []).filter((c) => attentionOf(c).length).length;
  const types = (view.agentTypes || []).map((t) => `${t.count} ${t.type}`).join(" · ");
  $("detail").innerHTML = `<div class="toolbar" role="toolbar" aria-label="Options">
    <span class="crumb">${options.embedded ? "Session" : '<a href="#/sessions">← Sessions</a>'} › <b>${esc(meta.project)}</b></span>
    ${options.embedded ? "" : '<button class="tb" type="button" data-reaudit>Re-audit</button>'}
    <details class="menu"><summary class="tb">Options ▾</summary><div class="mp">
      <button class="tb" type="button" data-copy title="Copies ${esc(resumeCommand(meta))}">Copy resume</button>
      <button class="tb" type="button" data-help>Keys <kbd>?</kbd></button>
    </div></details>
  </div>
  <header class="head">
    <div class="vseal" style="--c:${v.color}" aria-hidden="true">${v.seal}</div>
    <div>
      <div class="kick"><span class="stw" style="color:${v.color}">${v.glyph} ${esc(v.word)}</span> · ${esc(meta.project)} · ${esc(id.slice(0, 8))}</div>
      <h1>${esc(meta.title || "Session with no request")}</h1>
      <p class="lead">${esc(verdictLine(view))}</p>
      <div class="meta"><span>${meta.modified ? esc(new Date(meta.modified).toLocaleString("en-GB")) : ""}</span>${kind.agentSetting ? `<span>AGENT ${esc(kind.agentSetting)}</span>` : ""}${meta.runtime === "pi" ? "<span>pi</span>" : view.version ? `<span>claude code ${esc(view.version)}</span>` : ""}${kind.sessionKind ? `<span>${esc(kind.sessionKind)}</span>` : ""}</div>
    </div>
  </header>
  <div class="strip three" aria-label="Figures">
    <div><small>Active <span aria-hidden="true">時</span></small><b>${dur(s.active_ms)}</b><em>of ${dur(s.elapsed_ms)}</em></div>
    <div><small>Tokens <span aria-hidden="true">量</span></small><b>${tok(s.processed)}</b><em>${s.resumes || 0} resume(s)</em></div>
    <div><small>Sub-agents <span aria-hidden="true">隊</span></small><b>${s.subagents || 0}</b><em>${esc(types) || "none"}</em></div>
  </div>
  <div role="tablist" aria-label="Session sections">${TABS.map(([k, kanji, label, n], i) => { const c = n(view); const hot = (k === "hallazgos" && f.violation) ? " hot" : (k === "cambios" && look) ? " warn" : ""; return `<button role="tab" id="tab-${k}" aria-controls="p-${k}" aria-selected="${k === tab}" tabindex="${k === tab ? 0 : -1}" data-key="${i + 1}"><span class="jp" aria-hidden="true">${kanji}</span>${label}${c !== "" ? ` <span class="n${hot}">${c}</span>` : ""}</button>`; }).join("")}</div>
  <div class="db">
    <section role="tabpanel" id="p-resumen" aria-labelledby="tab-resumen"${tab === "resumen" ? "" : " hidden"}>${summaryPanel(view)}</section>
    <section role="tabpanel" id="p-cambios" aria-labelledby="tab-cambios"${tab === "cambios" ? "" : " hidden"}>${changesPanel(view)}</section>
    <section role="tabpanel" id="p-agentes" aria-labelledby="tab-agentes"${tab === "agentes" ? "" : " hidden"}>${agentsPanel(view)}</section>
    <section role="tabpanel" id="p-hallazgos" aria-labelledby="tab-hallazgos"${tab === "hallazgos" ? "" : " hidden"}>${findingsPanel(id, view)}<div class="sub">${readingPanel(view)}</div><div class="sub">${notesPanel(view)}</div></section>
    <section role="tabpanel" id="p-uso" aria-labelledby="tab-uso"${tab === "uso" ? "" : " hidden"}>${usagePanel(view, options.usage)}</section>
  </div>`;
  const tabs = [...document.querySelectorAll('[role="tab"]')];
  const select = (t, focus = true) => {
    for (const x of tabs) {
      const on = x === t;
      x.setAttribute("aria-selected", String(on));
      x.tabIndex = on ? 0 : -1;
      $(x.getAttribute("aria-controls")).hidden = !on;
    }
    if (focus) t.focus();
  };
  tabs.forEach((t, i) => {
    t.addEventListener("click", () => select(t));
    t.addEventListener("keydown", (e) => {
      if (e.key === "ArrowRight") select(tabs[(i + 1) % tabs.length]);
      if (e.key === "ArrowLeft") select(tabs[(i - 1 + tabs.length) % tabs.length]);
    });
  });
  for (const a of document.querySelectorAll("[data-go]")) {
    a.addEventListener("click", (e) => { e.preventDefault(); select($("tab-" + a.dataset.go)); });
  }
  bindAgents(view, select);
  for (const b of document.querySelectorAll("[data-review]")) {
    b.addEventListener("click", () => {
      const card = b.closest(".f");
      const on = !card.classList.contains("done");
      card.classList.toggle("done", on);
      b.textContent = on ? "Reviewed ✓" : "Mark reviewed";
      reviewed.set(id, b.dataset.review, on);
    });
  }
  document.querySelector("[data-copy]").addEventListener("click", async (e) => {
    const text = resumeCommand(meta);
    try { await navigator.clipboard.writeText(text); e.target.textContent = "Copied ✓"; } catch { window.prompt("Copy the command:", text); }
  });
  const reaudit = document.querySelector("[data-reaudit]");
  if (reaudit) reaudit.addEventListener("click", () => open(id, true));
  document.querySelector("[data-help]").addEventListener("click", () => toggleKeys(options.embedded));
  sessionKeys = (a) => {
    const t = a.do === "tab" && tabs.find((x) => x.dataset.key === a.n);
    if (t) select(t, false);
    else if (a.do === "reaudit" && reaudit) open(id, true);
  };
}

/** The selected tab's key, so a re-render keeps it. */
function currentTab() {
  const t = document.querySelector('[role="tab"][aria-selected="true"]');
  return t ? t.id.replace(/^tab-/, "") : "resumen";
}

/** The open session's keys (keyAction's answer): a tab by its number, re-audit. */
let sessionKeys = () => {};

// ── the served dashboard ────────────────────────────────────────────────────

const state = { sessions: [], q: "", current: "", filter: "all", uproject: "", mproject: "", mq: "", group: pref.get("group", "project"), sort: pref.get("sort", "recent"), range: pref.get("range", "7d"), tdays: Number(pref.get("tdays", "7")) || 7,
  f: { show: "", state: "", project: "", runtime: "", q: "", group: pref.get("group", "day"), sort: pref.get("sort", "recent"), scratch: pref.get("scratch", "") === "1" } };

const SORTS = { recent: (a, b) => Date.parse(b.modified) - Date.parse(a.modified), tokens: (a, b) => ((b.summary || {}).processed || 0) - ((a.summary || {}).processed || 0),
  findings: (a, b) => (((b.summary || {}).violation || 0) * 1e6 + ((b.summary || {}).suspect || 0)) - (((a.summary || {}).violation || 0) * 1e6 + ((a.summary || {}).suspect || 0)), size: (a, b) => b.bytes - a.bytes };

/** A scratch session: one run in a temporary directory (/tmp, a job's scratch), not a project's. */
export function isScratch(s) {
  return /(^|-)tmp(-|$)|-claude-jobs-|^-tmp-/.test(String(s.project || ""));
}

/**
 * Where a session came from: yours (you at Claude Code's prompt, or a pi
 * session), bridge (the Claude Code pi's model bridge runs for each pi turn:
 * the pi session shows it already), or headless (claude -p, an SDK's run).
 */
export function originOf(s) {
  if (s.runtime === "pi" || !s.entrypoint || s.entrypoint === "cli") return "yours";
  return s.entrypoint === "sdk-ts" ? "bridge" : "headless";
}

/** Whether the Sessions page's Show filter keeps s: yours ("" or "yours"), under the hood ("hood"), or all. */
const shows = (s, show) => show === "all" || (show === "hood" ? originOf(s) !== "yours" : originOf(s) === "yours");

/** The sessions the Sessions page shows: by origin, state, project, runtime, and text, scratch ones only when asked, sorted. */
export function filterSessions(sessions, f = {}) {
  const q = String(f.q || "").toLowerCase();
  return sessions.filter((s) => (f.scratch || !isScratch(s)) && shows(s, f.show) &&
    (!f.state || f.state === "all" || sessionState(s.summary) === f.state) &&
    (!f.project || s.name === f.project) &&
    (!f.runtime || s.runtime === f.runtime) &&
    (!q || [s.title, s.name, s.id, (s.summary || {}).change].some((x) => String(x || "").toLowerCase().includes(q))))
    .sort(SORTS[f.sort] || SORTS.recent);
}

/** The changes the sessions worked on, each with its latest session, newest first. */
export function changesInFlight(sessions) {
  const byChange = new Map();
  for (const s of sessions) {
    const c = (s.summary || {}).change;
    if (!c) continue;
    const seen = byChange.get(c);
    if (seen) seen.sessions++;
    if (!seen || Date.parse(s.modified) > Date.parse(seen.latest.modified)) byChange.set(c, { change: c, latest: s, sessions: seen ? seen.sessions : 1 });
  }
  return [...byChange.values()].sort((a, b) => Date.parse(b.latest.modified) - Date.parse(a.latest.modified));
}

/** One session as a row of the Sessions page and Home's lists: its seal and count, then its request, its state in words, and its change. */
export function sessionRow(s) {
  const st = sessionState(s.summary);
  const { glyph, word, seal, color } = STATES[st];
  const sum = s.summary || {};
  const n = st === "broken" ? sum.violation : st === "review" ? (sum.suspect || 0) + (sum.look || 0) : "";
  return `<a class="srow" data-row href="#/s/${esc(s.id)}" style="--c:${st === "pending" ? "var(--surface-hi)" : color}">
    <span class="sst" aria-hidden="true"><b>${seal}</b>${n ? `<small>${n}</small>` : ""}</span>
    <span class="stt"><b>${esc(s.title || "Session with no request")}</b><em><span class="stw" style="color:${color}">${glyph} ${esc(word)}</span>${sum.change ? ` · <span aria-hidden="true">◆</span> ${esc(sum.change)}` : ""}</em></span>
    <span class="spj">${esc(s.name)}</span>
    <span class="srt">${s.runtime === "pi" ? "π pi" : "claude"}</span>
    <span class="sn">${sum.active_ms ? dur(sum.active_ms) : "–"}</span>
    <span class="sn">${sum.processed ? tok(sum.processed) : "–"}</span>
    <span class="sw">${whenOf(s.modified)}</span>
  </a>`;
}

const sessionHead = '<div class="shead" aria-hidden="true"><span></span><span>Request</span><span>Project</span><span>Runtime</span><span class="n">Active</span><span class="n">Tokens</span><span class="n">When</span></div>';

/** The Sessions page: every session full width, filtered by state, project, runtime, and text, grouped by day or project. */
function renderSessions() {
  setNav("sessions");
  const f = state.f;
  const unscratched = state.sessions.filter((s) => f.scratch || !isScratch(s));
  const visible = unscratched.filter((s) => shows(s, f.show));
  const scratch = state.sessions.filter(isScratch).length;
  const hood = unscratched.filter((s) => originOf(s) !== "yours").length;
  const counts = { broken: 0, review: 0, pending: 0, clean: 0 };
  for (const s of visible) { const st = sessionState(s.summary); if (st in counts) counts[st]++; }
  const projects = new Map();
  for (const s of visible) projects.set(s.name, (projects.get(s.name) || 0) + 1);
  const chip = (attr, key, label, n, color) => `<button type="button" class="chip" data-${attr}="${esc(key)}" aria-pressed="${(f[attr] || "") === key}"${color ? ` style="--c:${color}"` : ""}>${color ? '<span class="d"></span>' : ""}${esc(label)}${n !== undefined ? ` <small>${n}</small>` : ""}</button>`;
  const shown = filterSessions(state.sessions, f);
  $("detail").innerHTML = `<div class="page">
    <div class="hrow"><div><div class="kick">SESSIONS · 履歴</div><h1>Your <em>sessions</em></h1>
      <p class="sub"><span id="shown">${shown.length} of ${visible.length}</span> shown. Open one to see its audit: what it did, what worked, and what to look at.</p></div>
      <label class="search big"><svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="11" cy="11" r="7"/><path d="M20 20l-3.5-3.5"/></svg><input id="q" type="search" placeholder="Search request, change, project or id" aria-label="Search sessions" value="${esc(f.q)}"><kbd>/</kbd></label></div>
    <div class="filters">
      <div class="frow"><span class="fl">Show</span>${chip("show", "", "Yours", unscratched.length - hood)}${chip("show", "hood", "Under the hood", hood)}${chip("show", "all", "All", unscratched.length)}<span class="fnote">${f.show === "hood" ? "the Claude Code pi's model bridge runs each turn (its pi session already shows it), and claude -p runs" : "your sessions: Claude Code at its prompt, and pi"}</span></div>
      <div class="frow"><span class="fl">State</span>${chip("state", "", "All", visible.length)}${chip("state", "broken", "Broke a rule", counts.broken, "var(--broken)")}${chip("state", "review", "Needs a look", counts.review, STATES.review.color)}${chip("state", "clean", "Clean", counts.clean, "var(--ok)")}${chip("state", "pending", "Not audited", counts.pending, "var(--faint)")}</div>
      <div class="frow"><span class="fl">Project</span>${chip("project", "", "All")}${[...projects].sort((a, b) => b[1] - a[1]).map(([p, n]) => chip("project", p, p, n)).join("")}</div>
      <div class="frow"><span class="fl">Runtime</span>${chip("runtime", "", "Both")}${chip("runtime", "claude-code", "Claude Code")}${chip("runtime", "pi", "π pi")}
        <span class="fsp"></span>
        <label class="sel">Group <select data-group-by>${[["day", "by day"], ["project", "by project"], ["none", "none"]].map(([v, l]) => `<option value="${v}"${f.group === v ? " selected" : ""}>${l}</option>`).join("")}</select></label>
        <label class="sel">Sort <select data-sort-by>${[["recent", "most recent"], ["tokens", "most tokens"], ["findings", "most findings"], ["size", "largest"]].map(([v, l]) => `<option value="${v}"${f.sort === v ? " selected" : ""}>${l}</option>`).join("")}</select></label>
        ${scratch ? `<label class="sel"><input type="checkbox" data-scratch${f.scratch ? " checked" : ""}> scratch sessions <small>${scratch}</small></label>` : ""}</div>
    </div>
    <div class="slist" id="slist">${sessionList(shown)}</div>
  </div>`;
  const d = $("detail");
  for (const attr of ["show", "state", "project", "runtime"]) for (const b of d.querySelectorAll(`[data-${attr}]`)) b.addEventListener("click", () => { f[attr] = b.dataset[attr]; renderSessions(); });
  d.querySelector("[data-group-by]").addEventListener("change", (e) => { f.group = e.target.value; pref.set("group", f.group); renderSessions(); });
  d.querySelector("[data-sort-by]").addEventListener("change", (e) => { f.sort = e.target.value; pref.set("sort", f.sort); renderSessions(); });
  const sc = d.querySelector("[data-scratch]");
  if (sc) sc.addEventListener("change", (e) => { f.scratch = e.target.checked; pref.set("scratch", f.scratch ? "1" : ""); renderSessions(); });
  // The search redraws the list alone, once the typing stops: the field keeps its focus and caret.
  const redraw = debounce(() => {
    if (!$("slist")) return;
    const list = filterSessions(state.sessions, f);
    $("slist").innerHTML = sessionList(list);
    $("shown").textContent = `${list.length} of ${visible.length}`;
  }, 150);
  $("q").addEventListener("input", (e) => { f.q = e.target.value; redraw(); });
}

/** The Sessions page's list: grouped by day or project, or not at all. */
function sessionList(shown) {
  const f = state.f;
  if (!shown.length) return '<p class="empty">No session matches these filters.</p>';
  let rows = "";
  if (f.group === "none") rows = shown.map(sessionRow).join("");
  else {
    const groups = new Map();
    for (const s of shown) { const k = f.group === "project" ? s.name : dayOf(s.modified); if (!groups.has(k)) groups.set(k, []); groups.get(k).push(s); }
    for (const [k, list] of groups) rows += `<div class="sgroup">${esc(k)} <span>${list.length}</span></div>` + list.map(sessionRow).join("");
  }
  return sessionHead + rows;
}

function heatLevel(n, max) { return n === 0 ? 0 : Math.min(4, Math.ceil((n / Math.max(1, max)) * 4)); }

/** Home: where to go (sessions, telemetry, changes), where you left off, what needs a look, and the range at a glance. */
function renderHome() {
  setNav("home");
  const real = state.sessions.filter((s) => !isScratch(s) && originOf(s) === "yours");
  const h = homeView(real, state.range);
  const rangeLabel = coverageLabel(state.range, state.coverage);
  const broken = h.queue.filter((x) => x.state === "broken").length, review = h.queue.length - broken;
  const changes = changesInFlight(real).slice(0, 5);
  const recent = [...real].sort(SORTS.recent).slice(0, 6);
  const t = state.telemetry;
  const tf = t ? telemetryFigures(t) : null;
  const tile = (href, kanji, title, big, text, hot) => `<a class="tile${hot ? " hot" : ""}" href="${href}"><span class="tk" aria-hidden="true">${kanji}</span><span class="tt">${title}</span><b>${big}</b><span class="tx">${text}</span><span class="tg">Open →</span></a>`;
  const queue = h.queue.slice(0, 5).map(({ s }) => sessionRow(s)).join("");
  const max = Math.max(0, ...h.heat.flat());
  const days = ["mon", "tue", "wed", "thu", "fri", "sat", "sun"];
  const heat = `<span></span>${Array.from({ length: 12 }, (_, i) => `<span>${i * 2}</span>`).join("")}` + h.heat.map((row, d) => `<span>${days[d]}</span>${row.map((n) => `<i class="l${heatLevel(n, max)}" title="${n} session(s)"></i>`).join("")}`).join("");
  $("detail").innerHTML = `<div class="page home">
    <div class="hero"><div class="kick">JHON · 監</div><h1>What do you want <em>to look at?</em></h1>
      <p class="sub">${state.coverage && state.coverage.capped ? `${real.length} of your sessions among the ${state.coverage.listed} most recent on this machine` : `${real.length} sessions on this machine`}${h.queue.length ? `, ${broken ? `${broken} broke a rule` : ""}${broken && review ? " and " : ""}${review ? `${review} ${review === 1 ? "needs" : "need"} a look` : ""} in ${rangeLabel}` : `, nothing needs a look in ${rangeLabel}`}.</p></div>
    <div class="tiles">
      ${tile("#/sessions", "履", "Sessions", real.length, `${h.queue.length ? `${h.queue.length} to review · ` : ""}every session's audit: what it did, what worked, what to look at`, broken)}
      ${tile("#/telemetry", "測", "Telemetry", tf ? t.events : "…", tf ? `event(s) today${tf.denies ? ` · ${tf.denies} refused` : ""} · what Jhon's guards, commands, and sub-agents did` : state.telemetryError ? `could not read it: ${esc(state.telemetryError)}` : "what Jhon's guards, commands, and sub-agents did", tf && tf.denies)}
      ${tile("#/changes", "変", "Changes", changes.length, changes.length ? `latest: ${esc(changes[0].change)} · every change across the sessions` : "no change in the audited sessions")}
    </div>
    <div class="cards">
      <section class="card span2" aria-labelledby="r-h"><div class="ch"><h2 id="r-h">Pick up where you left off${jp("続き")}</h2><a href="#/sessions">All sessions →</a></div><div class="cb slist">${recent.length ? sessionHead + recent.map(sessionRow).join("") : '<p class="muted">No session yet.</p>'}</div></section>
      <section class="card${h.queue.length ? " hot" : ""}" aria-labelledby="q-h"><div class="ch"><h2 id="q-h">Needs a look${jp("待ち")}</h2>${h.queue.length > 5 ? `<a href="#/sessions" data-show-review>See all ${h.queue.length} →</a>` : ""}</div><div class="cb slist compact">${queue || '<p class="muted">Nothing to review in this range.</p>'}</div></section>
      <section class="card" aria-labelledby="c-h"><div class="ch"><h2 id="c-h">Changes${jp("変更")}</h2><a href="#/changes">All changes →</a></div><div class="cb">${changes.length ? `<ul class="clist">${changes.map((c) => `<li><a data-row href="${esc(changeHref(c.latest.name, c.change))}"><b><span aria-hidden="true">◆</span> ${esc(c.change)}</b><span>${esc(c.latest.name)} · ${c.sessions} session(s) · ${whenOf(c.latest.modified)}</span></a></li>`).join("")}</ul>` : '<p class="muted">No audited session names a change.</p>'}</div></section>
    </div>
    <div class="hrow sub2"><h2>At a glance <span aria-hidden="true">全体</span></h2>
      <div class="range seg" role="group" aria-label="Range">${[["hoy", "Today"], ["7d", "7 days"], ["30d", "30 days"], ["todo", "All"]].map(([v, l]) => `<button type="button" data-range="${v}" aria-pressed="${state.range === v}">${l}</button>`).join("")}</div></div>
    <section class="kpis" aria-label="Figures">
      <div><small>Sessions <span aria-hidden="true">数</span></small><b>${h.sessions}</b><em>in ${h.projects} project(s)${h.pending ? ` · ${h.pending} not audited yet` : ""}</em></div>
      <div><small>Active time <span aria-hidden="true">時</span></small><b>${dur(h.active_ms)}</b><em>of ${dur(h.elapsed_ms)} open</em></div>
      <div><small>Tokens <span aria-hidden="true">量</span></small><b>${tok(h.processed)}</b><em>${h.processed ? pct(h.outside / h.processed) : "0%"} outside changes</em></div>
      <div><small>Broken rules <span aria-hidden="true">破</span></small><b style="color:${h.broken ? "var(--broken)" : "inherit"}">${h.broken}</b><em>${h.brokenChecks.length ? esc(h.brokenChecks.map((c) => c.title.toLowerCase()).join(", ")) : "none"}</em></div>
    </section>
    ${integrationsCard(state.integrations, state.integrationsError)}
    <section class="card" aria-labelledby="h-h"><div class="ch"><h2 id="h-h">Activity${jp("活動")}</h2><span class="ab">sessions per 2h slot</span></div><div class="cb"><div class="heat" role="img" aria-label="Sessions by day and time slot">${heat}</div><div class="hl">less <i class="l0"></i><i class="l1"></i><i class="l2"></i><i class="l3"></i><i class="l4"></i> more</div></div></section>
  </div>`;
  for (const b of $("detail").querySelectorAll("[data-range]")) b.addEventListener("click", () => { state.range = b.dataset.range; pref.set("range", state.range); renderHome(); });
  const all = $("detail").querySelector("[data-show-review]");
  if (all) all.addEventListener("click", () => { state.f.state = "review"; });
}

/** An error in place of a page, with a retry: a failed read is never shown as nothing. */
function showError(title, message, retry) {
  $("detail").innerHTML = `<div class="welcome" role="alert"><div><span class="seal" aria-hidden="true">誤</span><h1>${esc(title)}</h1><p>${esc(message)}</p><p><button class="btn primary" type="button" data-retry>Try again</button></p></div></div>`;
  $("detail").querySelector("[data-retry]").addEventListener("click", retry);
}

/** A page's heading while its data loads, over a skeleton. */
const loading = (kick, title, rows = 6) => `<div class="page"><div class="hrow"><div><div class="kick">${kick}</div><h1>${title}</h1></div></div>${skeleton(rows)}</div>`;

/** The top bar's current page. */
function setNav(page) {
  for (const a of document.querySelectorAll(".nav a")) a.setAttribute("aria-current", String(a.dataset.page === page));
}

/** The Telemetry view's headline figures from `/api/telemetry`: refusals, passing test runs, ready checks, sub-agents at their cap, the slowest hook. */
export function telemetryFigures(t) {
  const hooks = t.hooks || [];
  const calls = hooks.reduce((n, h) => n + h.count, 0);
  const denies = hooks.reduce((n, h) => n + h.deny, 0);
  const agents = t.subagents || [];
  const runs = agents.reduce((n, a) => n + a.count, 0);
  const capped = agents.reduce((n, a) => n + a.turn_cap, 0);
  const slow = hooks.filter((h) => h.p95_ms != null).sort((a, b) => b.p95_ms - a.p95_ms)[0];
  return {
    decisions: calls, denies, denyShare: calls ? denies / calls : 0,
    tests: t.tests || { runs: 0, green: 0 }, deliveries: t.deliveries || { checks: 0, ready: 0 },
    subagents: runs, capped, cappedShare: runs ? capped / runs : 0,
    slowest: slow ? { name: slow.name, p95_ms: slow.p95_ms } : null,
  };
}

const msText = (ms) => ms == null ? "-" : ms < 1000 ? `${Math.round(ms)}ms` : secs(ms);

async function renderTelemetry() {
  setNav("telemetry");
  const days = state.tdays;
  $("detail").innerHTML = loading("TELEMETRY · 測定", "Reading the <em>telemetry</em>…", 4);
  const got = await loadJSON(`/api/telemetry?days=${days}`);
  if (location.hash !== "#/telemetry" || state.tdays !== days) return;
  if (got.error) return showError("Could not read the telemetry", got.error, renderTelemetry);
  const t = got.data;
  const f = telemetryFigures(t);
  const hooks = (t.hooks || []).map((h) => `<tr><td><code>${esc(h.name)}</code></td><td class="n">${h.count}</td><td class="n">${h.allow}</td><td class="n"${h.deny ? ' style="color:var(--broken)"' : ""}>${h.deny}</td><td class="n">${h.ask}</td><td class="n">${h.context}</td><td class="n">${h.error}</td><td class="n">${msText(h.p50_ms)}</td><td class="n">${msText(h.p95_ms)}</td></tr>`);
  const agents = (t.subagents || []).map((a) => `<tr><td><code>${esc(a.agent)}</code></td><td class="n">${a.count}</td><td class="n">${a.completed}</td><td class="n"${a.turn_cap ? ' style="color:var(--gold)"' : ""}>${a.turn_cap}</td><td class="n">${a.timeout + a.cancelled}</td><td class="n"${a.failed ? ' style="color:var(--broken)"' : ""}>${a.failed}</td><td class="n">${msText(a.p50_ms)}</td><td class="n">${a.p50_turns ?? "-"}</td></tr>`);
  const commands = (t.commands || []).map((c) => `<tr><td><code>${esc(`${c.command} ${c.verb}`.trim())}</code></td><td class="n">${c.count}</td><td class="n"${c.failed ? ' style="color:var(--gold)"' : ""}>${c.failed}</td><td class="n">${msText(c.p50_ms)}</td></tr>`);
  const denials = (t.denials || []).map((d) => `<li style="--c:var(--broken)"><span class="k" aria-hidden="true">止</span><div><b>${esc(d.hook)} · ${esc(d.agent || "main")}${d.tool ? ` · ${esc(d.tool)}` : ""}</b><p>${esc(d.reason)}</p></div><span class="go">${whenOf(d.time)}</span></li>`).join("");
  const top = Math.max(1, ...(t.per_day || []).map((d) => d.events));
  const perDay = (t.per_day || []).map((d) => `<div class="pr"><span class="n">${esc(d.day)}</span><span class="pbar"><i style="width:${(d.events / top * 100).toFixed(1)}%"></i></span><span class="v">${d.events}</span>${d.denies ? `<span class="s" style="color:var(--broken)">止 ${d.denies}</span>` : ""}</div>`).join("");
  const empty = !t.events;
  $("detail").innerHTML = `<div class="page home">
    <div class="hrow"><div><div class="kick">TELEMETRY · 測定</div><h1>What Jhon <em>decided and ran</em></h1>
      <p class="sub">${t.events} event(s) in the last ${t.days} day(s), recorded by Jhon itself: its guards, its commands, the pi shell's sub-agents and compactions.${t.skipped ? ` ${t.skipped} unreadable line(s) skipped.` : ""}</p></div>
      <div class="range seg" role="group" aria-label="Range">${[[1, "Today"], [7, "7 days"], [30, "30 days"]].map(([v, l]) => `<button type="button" data-tdays="${v}" aria-pressed="${days === v}">${l}</button>`).join("")}</div></div>
    ${empty ? '<p class="muted">Nothing recorded in this range. Events arrive as Jhon works: each guard decision, each test run and delivery check, each sub-agent pi runs.</p>' : `
    <section class="kpis" aria-label="Figures">
      <div><small>Guard decisions <span aria-hidden="true">守</span></small><b>${f.decisions}</b><em>${f.denies} refused (${pct(f.denyShare)})</em></div>
      <div><small>Test runs <span aria-hidden="true">検</span></small><b>${f.tests.green}/${f.tests.runs}</b><em>passed · ${f.deliveries.ready}/${f.deliveries.checks} delivery checks ready</em></div>
      <div><small>Sub-agents <span aria-hidden="true">隊</span></small><b>${f.subagents}</b><em>${f.capped} at their turn cap (${pct(f.cappedShare)})</em></div>
      <div><small>Slowest hook <span aria-hidden="true">遅</span></small><b>${f.slowest ? msText(f.slowest.p95_ms) : "-"}</b><em>${f.slowest ? `${esc(f.slowest.name)}, p95` : "no duration recorded"}</em></div>
    </section>
    <div class="cards">
      <section class="card span2" aria-labelledby="g-h"><div class="ch"><h2 id="g-h">Guards${jp("守")}</h2><span class="ab">every decision, and what it cost each call</span></div><div class="cb">${table([["Hook"], ["Calls", 1], ["Allow", 1], ["Deny", 1], ["Ask", 1], ["Note", 1], ["Error", 1], ["p50", 1], ["p95", 1]], hooks)}</div></section>
      <section class="card span2" aria-labelledby="sa-h"><div class="ch"><h2 id="sa-h">Sub-agents${jp("隊")}</h2><span class="ab">pi's runs and how they ended</span></div><div class="cb">${table([["Agent"], ["Runs", 1], ["Done", 1], ["Turn cap", 1], ["Stopped", 1], ["Failed", 1], ["p50 time", 1], ["p50 turns", 1]], agents)}</div></section>
      <section class="card" aria-labelledby="c-h"><div class="ch"><h2 id="c-h">Commands${jp("令")}</h2><span class="ab">what acted or decided</span></div><div class="cb">${table([["Command"], ["Runs", 1], ["Failed", 1], ["p50", 1]], commands)}</div></section>
      <section class="card${denials ? " hot" : ""}" aria-labelledby="d-h"><div class="ch"><h2 id="d-h">Latest refusals${jp("止")}</h2><span class="ab">newest first</span></div><div class="cb">${denials ? `<ul class="att">${denials}</ul>` : '<p class="muted">No guard refused a call.</p>'}</div></section>
      <section class="card span2" aria-labelledby="pd-h"><div class="ch"><h2 id="pd-h">By day${jp("日")}</h2><span class="ab">events, and refusals</span></div><div class="cb">${perDay}</div></section>
    </div>`}</div>`;
  for (const b of $("detail").querySelectorAll("[data-tdays]")) b.addEventListener("click", () => { state.tdays = Number(b.dataset.tdays); pref.set("tdays", String(state.tdays)); renderTelemetry(); });
}

/** The projects of the owner's own sessions, most sessions first: the Usage page's chips, as the Sessions page's. */
function ownProjects() {
  const n = new Map();
  for (const s of state.sessions) if (!isScratch(s) && originOf(s) === "yours") n.set(s.name, (n.get(s.name) || 0) + 1);
  return [...n].sort((a, b) => b[1] - a[1]);
}

/** The Usage page (L138, answer 2): the owner's usage per day in Home's range and one project or all, its figures and its models. */
async function renderUsage() {
  setNav("usage");
  const url = usageQuery(state.range, state.uproject);
  if (state.usageUrl !== url) $("detail").innerHTML = loading("USAGE · 使用", "Your <em>usage</em>", 6);
  const got = await loadJSON(url);
  if (location.hash !== "#/usage" || usageQuery(state.range, state.uproject) !== url) return;
  if (got.error) return showError("Could not read the usage", got.error, renderUsage);
  state.usageUrl = url;
  const u = got.data;
  const rangeLabel = coverageLabel(state.range, u);
  const chip = (key, label, n) => `<button type="button" class="chip" data-uproject="${esc(key)}" aria-pressed="${state.uproject === key}">${esc(label)}${n !== undefined ? ` <small>${n}</small>` : ""}</button>`;
  const figures = usageFigures(u).map((f) => `<div><small>${esc(f.label)}</small><b>${f.href ? `<a href="${esc(f.href)}">${esc(f.value)}</a>` : esc(f.value)}</b><em title="${esc(f.note)}">${esc(f.note)}</em></div>`).join("");
  const top = Math.max(1, ...u.days.map((d) => d.total));
  const days = [...u.days].reverse().map((d) => `<div class="pr"><span class="n">${esc(monthDay(d.day))} <span class="muted">${WEEKDAYS[new Date(`${d.day}T00:00`).getDay()].toLowerCase()}</span></span><span class="pbar" role="img" aria-label="${esc(tok(d.total))} tokens"><i style="width:${(d.total / top * 100).toFixed(1)}%"></i></span><span class="v">${tok(d.total)}</span><span class="m">${esc(money(d.cost))}</span></div>`).join("");
  const models = u.models.map((m) => `<tr><td><code>${esc(m.model)}</code></td><td class="n">${meter(pct(m.share), m.share)}</td><td class="n">${tok(m.total)}</td><td class="n">${esc(money(m.cost))}</td><td class="n">${m.messages}</td></tr>`);
  $("detail").innerHTML = `<div class="page home">
    <div class="hrow"><div><div class="kick">USAGE · 使用</div><h1>Your <em>usage</em></h1>
      <p class="sub">${tok(u.totals.total)} tokens, ${esc(money(u.totals.cost, u.cost_covers))}, in ${rangeLabel}${state.uproject ? ` in ${esc(state.uproject)}` : ""}: what your sessions' transcripts record, their sub-agents' included. Money is what pi recorded; Claude Code records none, and nothing is estimated.</p></div>
      <div class="range seg" role="group" aria-label="Range">${[["hoy", "Today"], ["7d", "7 days"], ["30d", "30 days"], ["todo", "All"]].map(([v, l]) => `<button type="button" data-range="${v}" aria-pressed="${state.range === v}">${l}</button>`).join("")}</div></div>
    <div class="filters"><div class="frow"><span class="fl">Project</span>${chip("", "All")}${ownProjects().map(([p, n]) => chip(p, p, n)).join("")}</div></div>
    <section class="kpis six" aria-label="Figures">${figures}</section>
    <div class="cards">
      <section class="card" aria-labelledby="ud-h"><div class="ch"><h2 id="ud-h">By day${jp("日")}</h2><span class="ab">tokens, and money where pi recorded it; newest first</span></div><div class="cb">${days || '<p class="muted">No usage recorded in this range.</p>'}</div></section>
      <section class="card" aria-labelledby="um-h"><div class="ch"><h2 id="um-h">Models${jp("型")}</h2><span class="ab">most tokens first</span></div><div class="cb">${table([["Model"], ["Share", 1], ["Tokens", 1], ["Money", 1], ["Messages", 1]], models)}<p class="muted">${tok(u.totals.total)} tokens · ${esc(money(u.totals.cost, u.cost_covers))} in ${plural(u.sessions, "session", "sessions")}</p></div></section>
    </div></div>`;
  for (const b of $("detail").querySelectorAll("[data-range]")) b.addEventListener("click", () => { state.range = b.dataset.range; pref.set("range", state.range); renderUsage(); });
  for (const b of $("detail").querySelectorAll("[data-uproject]")) b.addEventListener("click", () => { state.uproject = b.dataset.uproject; renderUsage(); });
}

async function open(id, fresh = false) {
  const tab = fresh ? currentTab() : "resumen";
  state.current = id;
  setNav("sessions");
  $("detail").innerHTML = `<div class="welcome" aria-busy="true"><div><span class="seal" aria-hidden="true">監</span><h1>Auditing the session…</h1><p>The audit is being built now; a long session takes a few seconds.</p>${skeleton(4)}</div></div>`;
  const got = await loadJSON("/api/session/" + encodeURIComponent(id) + (fresh ? "?fresh=1" : ""));
  if (state.current !== id) return;
  if (got.error) return showError("Could not open the session", got.error, () => open(id, fresh));
  const { session, view, usage } = got.data;
  renderSession(session, view, tab, { usage });
}

/** Every change the sessions name (/api/changes), read again after 30 s at most; {error} kept when the read failed. */
async function loadChanges(force = false) {
  if (!force && state.changes && Date.now() - state.changesAt < 30000) return;
  const got = await loadJSON("/api/changes");
  state.changesAt = Date.now();
  if (got.error) { state.changesError = got.error; return; }
  state.changes = got.data;
  state.changesError = "";
}

/** The Changes page's column heads: slices, last run and route are the project's state today. */
export const CHANGE_HEAD = '<div class="chead" aria-hidden="true"><span>Change</span><span>Last session</span><span>Slices today</span><span>Last run today</span><span>Route</span></div>';

/** One change as a row of the Changes page. */
function changeRow(c) {
  const f = changeFacts(c);
  const last = (c.sessions || [])[0] || {};
  return `<a class="crow" data-row href="${esc(changeHref(c.project, c.name))}">
    <span class="cnm"><b><span aria-hidden="true">◆</span> ${esc(c.name)}</b><em>${esc(c.project)} · ${(c.sessions || []).length} session(s)</em></span>
    <span class="cls"><b>${esc(last.title || "Session with no request")}</b><em>${last.modified ? whenOf(last.modified) : "-"}</em></span>
    <span class="csl">${f.slices ? esc(f.slices) : '<span class="muted">-</span>'}</span>
    <span class="crn">${f.run ? `<span style="color:${f.run.color}">${f.run.mark} ${esc(f.run.text)}</span>` : '<span class="muted">no run</span>'}</span>
    <span class="crt">${f.route ? `<span class="pill">${esc(f.route)}</span>` : '<span class="muted">-</span>'}</span>
  </a>`;
}

/** The Changes page: every change seen across the sessions, newest first. */
async function renderChanges() {
  setNav("changes");
  if (!state.changes) $("detail").innerHTML = loading("CHANGES · 変更", "Every <em>change</em>");
  await loadChanges();
  if (location.hash !== "#/changes") return;
  if (state.changesError && !state.changes) return showError("Could not read the changes", state.changesError, () => { state.changes = null; renderChanges(); });
  const list = state.changes;
  $("detail").innerHTML = `<div class="page">
    <div class="hrow"><div><div class="kick">CHANGES · 変更</div><h1>Every <em>change</em></h1>
      <p class="sub">${list.length} change(s) across the audited sessions, newest first. Slices, last run and route are the project's state today.${state.changesError ? ` Could not read them again: ${esc(state.changesError)}.` : ""}</p></div></div>
    <div class="slist clist2">${list.length ? CHANGE_HEAD + list.map(changeRow).join("") : '<p class="empty">No audited session names a change yet.</p>'}</div>
  </div>`;
}

/** One change: its state in the project today when known, and its sessions, newest first. */
async function renderChange(project, name) {
  setNav("changes");
  if (!state.changes) $("detail").innerHTML = loading("CHANGE · 変", esc(name), 4);
  await loadChanges();
  const here = location.hash.match(CHANGE_ROUTE);
  if (!here || decodeURIComponent(here[1]) !== project || decodeURIComponent(here[2]) !== name) return;
  if (state.changesError && !state.changes) return showError("Could not read the changes", state.changesError, () => { state.changes = null; renderChange(project, name); });
  const c = state.changes.find((x) => x.project === project && x.name === name);
  if (!c) {
    $("detail").innerHTML = `<div class="welcome"><div><span class="seal" aria-hidden="true">無</span><h1>No session names ${esc(name)}</h1><p><a href="#/changes">← Changes</a></p></div></div>`;
    return;
  }
  const f = changeFacts(c);
  const rows = (c.sessions || []).map((x) => sessionRow(state.sessions.find((s) => s.id === x.id) || { ...x, name: c.project })).join("");
  $("detail").innerHTML = `<div class="page">
    <div class="hrow"><div><div class="kick"><a href="#/changes">CHANGES</a> · ${esc(c.project)} · 変</div><h1><em>${esc(c.name)}</em></h1>
      <p class="sub">${(c.sessions || []).length} session(s)${f.route ? ` · route ${esc(f.route)}` : ""}${f.slices ? ` · ${esc(f.slices)}` : ""}${f.run ? ` · last run <span style="color:${f.run.color}">${f.run.mark} ${esc(f.run.text)}</span>` : ""}</p></div></div>
    ${c.ldd ? lddCard(c.name, c.ldd) : '<p class="muted">Its project no longer holds it as an active change (archived, or gone): its sessions are its history.</p>'}
    <section class="card" aria-labelledby="cs-h"><div class="ch"><h2 id="cs-h">Sessions${jp("履歴")}</h2><span class="ab">newest first</span></div><div class="cb slist">${sessionHead + rows}</div></section>
  </div>`;
}

const CHANGE_ROUTE = /^#\/c\/([^/]+)\/([^/]+)$/;

function route() {
  const m = location.hash.match(/^#\/s\/([0-9a-f-]+)$/i);
  if (m) return open(m[1]);
  state.current = "";
  window.scrollTo?.(0, 0);
  // Esc from a session goes back to the list it was opened from.
  state.from = location.hash || "#/";
  const c = location.hash.match(CHANGE_ROUTE);
  if (c) return renderChange(decodeURIComponent(c[1]), decodeURIComponent(c[2]));
  if (location.hash === "#/telemetry") return renderTelemetry();
  if (location.hash === "#/changes") return renderChanges();
  if (location.hash === "#/usage") return renderUsage();
  if (state.sessionsError) return showError("Could not read the sessions", state.sessionsError, retrySessions);
  if (location.hash === "#/sessions") return renderSessions();
  const mem = memoryRoute(location.hash);
  if (mem) return renderMemory(mem);
  renderHome();
  loadTelemetry();
  loadIntegrations();
}

/** Today's telemetry for Home's tile, fetched once a minute at most; Home says when it could not be read. */
async function loadTelemetry() {
  if (state.telemetryAt && Date.now() - state.telemetryAt < 60000) return;
  state.telemetryAt = Date.now();
  const got = await loadJSON("/api/telemetry?days=1");
  if (got.error) state.telemetryError = got.error;
  else { state.telemetry = got.data; state.telemetryError = ""; }
  if (!location.hash || location.hash === "#" || location.hash === "#/") renderHome();
}

/** The integrations for Home's block (/api/integrations), read once a minute at most; Home says when they could not be read. */
async function loadIntegrations() {
  if (state.integrationsAt && Date.now() - state.integrationsAt < 60000) return;
  state.integrationsAt = Date.now();
  const got = await loadJSON("/api/integrations");
  if (got.error) state.integrationsError = got.error;
  else { state.integrations = got.data; state.integrationsError = ""; }
  if (!location.hash || location.hash === "#" || location.hash === "#/") renderHome();
}

/** The sessions read again; a failed read keeps the list it had, and with none it is the page's error. */
async function loadSessions() {
  const [got, cov] = await Promise.all([loadJSON("/api/sessions"), loadJSON("/api/coverage")]);
  if (cov.data) state.coverage = cov.data;
  if (got.error) { if (!state.loaded) state.sessionsError = got.error; return false; }
  state.sessions = got.data;
  state.sessionsError = "";
  state.loaded = true;
  return true;
}

async function retrySessions() {
  $("detail").innerHTML = skeleton(8);
  await loadSessions();
  route();
}

/** The list again, with the summaries the background audits added; Inicio redraws when it is open. */
async function refreshSessions() {
  if (!(await loadSessions())) return;
  if (state.current) return;
  if (location.hash === "#/sessions") renderSessions();
  else if (!location.hash || location.hash === "#" || location.hash === "#/") renderHome();
}

// ── the memory page: read only (L138, answer 1) ─────────────────────────────

/** The projects the Memory page offers: those of your own sessions (not scratch), the most recent session's first, which is the default. */
export function memoryProjects(sessions) {
  const by = new Map();
  for (const s of [...sessions].sort(SORTS.recent)) {
    if (isScratch(s) || originOf(s) !== "yours") continue;
    const p = by.get(s.project);
    if (p) p.sessions++;
    else by.set(s.project, { dir: s.project, name: s.name, sessions: 1 });
  }
  return [...by.values()];
}

/** Engram's observation types in words. */
const TYPE_WORDS = { session_summary: "session summary", manual: "note" };

/** An Engram observation as a row: its title, then its type in words, its id and its date. */
export function observationLine(o) {
  const at = o.created_at ? ` · ${String(o.created_at).replace("T", " ").slice(0, 16)}` : "";
  return { title: o.title || "Untitled", detail: `${TYPE_WORDS[o.type] || o.type} · #${o.id}${at}` };
}

/** Why Engram does not answer, from a failed read of /api/memory/engram (502); null for any other failure. */
export function engramDown(error) {
  const m = /^502: (Engram (?:does not answer: |answered )[\s\S]*)$/.exec(String(error || ""));
  return m ? m[1].trim() : null;
}

/** Where `/` searches from the page at hash: Engram on the Memory pages, the sessions elsewhere. */
export const searchTarget = (hash) => (/^#\/memory(\/|$)/.test(hash) ? "#/memory" : "#/sessions");

/** The Memory page's address: the list, one observation by its id, or one file by its name; null when hash is not the Memory page's. */
export function memoryRoute(hash) {
  if (hash === "#/memory") return { kind: "list" };
  const m = /^#\/memory\/(o|f)\/([^/]+)$/.exec(hash);
  if (!m || (m[1] === "o" && !/^\d+$/.test(m[2]))) return null;
  return m[1] === "o" ? { kind: "engram", id: m[2] } : { kind: "file", name: decodeURIComponent(m[2]) };
}

/** A list of memory rows, each opening its address. */
const memoryRows = (rows) => `<ul class="clist">${rows.map((r) => `<li><a data-row href="${esc(r.href)}"><b>${esc(r.title)}</b><span>${esc(r.detail)}</span></a></li>`).join("")}</ul>`;

/** The Engram card: the total, the newest or a search's results, or why Engram does not answer. */
function engramCard(got) {
  const q = state.mq;
  if (got.error) {
    const down = engramDown(got.error);
    return `<div role="alert"><p style="color:var(--broken)">✗ ${down !== null ? esc(down) : `Could not read Engram: ${esc(got.error)}`}</p><p class="muted">Engram is read through its local API (ENGRAM_URL, else its default port). Start it with <code>engram serve</code>, then try again.</p><p><button class="btn" type="button" data-mretry>Try again</button></p></div>`;
  }
  const e = got.data;
  const head = `<p class="muted">${esc(e.project)}${e.total != null ? ` · ${e.total} observation(s)` : ""} · ${q ? `${e.observations.length} result(s) for «${esc(q)}» <button class="tb" type="button" data-mclear>Clear</button>` : "most recent first"}</p>`;
  const rows = e.observations.map((o) => ({ ...observationLine(o), href: `#/memory/o/${o.id}` }));
  return head + (rows.length ? memoryRows(rows) : `<p class="muted">${q ? `No observation matches «${esc(q)}».` : "Nothing saved yet."}</p>`);
}

/** The Files card: MEMORY.md's index, in its order. */
function filesCard(got) {
  if (got.error) return `<div role="alert"><p style="color:var(--broken)">✗ Could not read the file memory: ${esc(got.error)}</p><p><button class="btn" type="button" data-mretry>Try again</button></p></div>`;
  const f = got.data;
  const head = `<p class="muted">${f.files.length} in <code>${esc(f.dir)}</code> · the MEMORY.md index, in its order</p>`;
  return head + (f.files.length ? memoryRows(f.files.map((x) => ({ title: x.title, detail: x.hook ? `${x.file} · ${x.hook}` : x.file, href: `#/memory/f/${encodeURIComponent(x.file)}` }))) : '<p class="muted">No file memory for this project.</p>');
}

/** The Memory page: a project's Engram observations and its file memory, read only; or one of them whole. */
async function renderMemory(at) {
  setNav("memory");
  const projects = memoryProjects(state.sessions);
  if (!projects.some((p) => p.dir === state.mproject)) state.mproject = projects.length ? projects[0].dir : "";
  const dir = state.mproject;
  const project = projects.find((p) => p.dir === dir);
  if (!dir) {
    $("detail").innerHTML = '<div class="page"><div class="hrow"><div><div class="kick">MEMORY · 記憶</div><h1>What a project <em>remembers</em></h1><p class="sub">No session yet, so no project to read the memory of.</p></div></div></div>';
    return;
  }
  const here = location.hash;
  if (at.kind !== "list") {
    $("detail").innerHTML = loading(`MEMORY · ${esc(project.name)}`, "Opening…", 4);
    const got = at.kind === "engram" ? await loadJSON(`/api/memory/engram/${at.id}`)
      : await loadJSON(`/api/memory/file?project=${encodeURIComponent(dir)}&name=${encodeURIComponent(at.name)}`);
    if (location.hash !== here) return;
    if (got.error) return showError(at.kind === "engram" ? `Could not open observation #${at.id}` : `Could not open ${at.name}`, engramDown(got.error) ?? got.error, () => renderMemory(at));
    const o = got.data;
    const title = at.kind === "engram" ? o.title || "Untitled" : at.name;
    const meta = at.kind === "engram" ? `${observationLine(o).detail}${o.topic_key ? ` · ${o.topic_key}` : ""}` : `${project.name} · file memory`;
    $("detail").innerHTML = `<div class="page"><div class="hrow"><div><div class="kick"><a href="#/memory">MEMORY</a> · ${esc(project.name)} · 記憶</div><h1>${esc(title)}</h1><p class="sub">${esc(meta)}</p></div></div>
      <section class="card"><div class="cb"><pre class="mtext">${esc(at.kind === "engram" ? o.content : o.text)}</pre></div></section></div>`;
    return;
  }
  const picker = `<label class="sel">Project <select data-mproject aria-label="Project">${projects.map((p) => `<option value="${esc(p.dir)}"${p.dir === dir ? " selected" : ""}>${esc(p.name)} (${p.sessions})</option>`).join("")}</select></label>`;
  $("detail").innerHTML = `<div class="page">
    <div class="hrow"><div><div class="kick">MEMORY · 記憶</div><h1>What <em>${esc(project.name)}</em> remembers</h1>
      <p class="sub">Read only: Engram's observations of the project, and the file memory Claude Code and pi share. Open one to read it whole. ${esc(memoryPickerNote(state.coverage))}</p></div>${picker}</div>
    <div class="cards">
      <section class="card" aria-labelledby="me-h"><div class="ch"><h2 id="me-h">Engram${jp("記録")}</h2><label class="search sm"><input id="mq" type="search" placeholder="Search Engram" aria-label="Search Engram" value="${esc(state.mq)}"><kbd>/</kbd></label></div><div class="cb" id="mengram">${skeleton(4)}</div></section>
      <section class="card" aria-labelledby="mf-h"><div class="ch"><h2 id="mf-h">Files${jp("文書")}</h2><span class="ab">MEMORY.md</span></div><div class="cb" id="mfiles">${skeleton(4)}</div></section>
    </div></div>`;
  const d = $("detail");
  d.querySelector("[data-mproject]").addEventListener("change", (ev) => { state.mproject = ev.target.value; state.mq = ""; renderMemory(at); });
  $("mq").addEventListener("keydown", ({ key, target }) => { if (key === "Enter") { state.mq = target.value.trim(); drawEngram(); } });
  const bindRetry = () => { for (const b of d.querySelectorAll("[data-mretry]")) b.addEventListener("click", () => renderMemory(at)); };
  const drawEngram = async () => {
    const q = state.mq;
    const got = await loadJSON(`/api/memory/engram?project=${encodeURIComponent(dir)}${q ? `&q=${encodeURIComponent(q)}` : ""}`);
    if (location.hash !== here || state.mproject !== dir || state.mq !== q || !$("mengram")) return;
    $("mengram").innerHTML = engramCard(got);
    const clear = $("mengram").querySelector("[data-mclear]");
    if (clear) clear.addEventListener("click", () => { state.mq = ""; $("mq").value = ""; drawEngram(); });
    bindRetry();
  };
  const drawFiles = async () => {
    const got = await loadJSON(`/api/memory/files?project=${encodeURIComponent(dir)}`);
    if (location.hash !== here || state.mproject !== dir || !$("mfiles")) return;
    $("mfiles").innerHTML = filesCard(got);
    bindRetry();
  };
  await Promise.all([drawEngram(), drawFiles()]);
}

// ── end of the memory page ──────────────────────────────────────────────────

// ── keys and the command bar ────────────────────────────────────────────────

/** The rows j and k move through: the lists' rows on the page. */
const rowsOnPage = () => [...document.querySelectorAll("#detail [data-row]")].filter((r) => r.offsetParent !== null);

function moveRow(by) {
  const rows = rowsOnPage();
  if (!rows.length) return;
  const at = rows.findIndex((r) => r.classList.contains("on"));
  const next = at < 0 ? (by > 0 ? 0 : rows.length - 1) : Math.max(0, Math.min(rows.length - 1, at + by));
  if (at >= 0) rows[at].classList.remove("on");
  rows[next].classList.add("on");
  rows[next].focus({ preventScroll: true });
  rows[next].scrollIntoView?.({ block: "nearest" });
}

/** Opens the selected row; false when none is selected. */
function openRow() {
  const row = document.querySelector("#detail [data-row].on");
  if (!row) return false;
  location.hash = row.getAttribute("href");
  return true;
}

/** Esc: closes what is open, else goes up: a session to the list it came from, a change to the changes, a page home. */
function back() {
  if ($("keys") && !$("keys").hidden) return toggleKeys(false, false);
  const h = location.hash;
  if (/^#\/s\//.test(h)) location.hash = state.from && state.from !== h ? state.from : "#/sessions";
  else if (CHANGE_ROUTE.test(h)) location.hash = "#/changes";
  else if (h.startsWith("#/memory/")) location.hash = "#/memory";
  else if (h && h !== "#" && h !== "#/") location.hash = "#/";
}

const bar = { open: false, q: "", sel: 0, all: [] };

/** The command bar (design 03): finds a session or a change by its text, and goes to a page. */
function openBar() {
  if (bar.open) return closeBar();
  Object.assign(bar, { open: true, q: "", sel: 0, back: document.activeElement });
  const el = Object.assign(document.createElement("div"), { id: "bar", className: "cbar" });
  el.setAttribute("role", "dialog");
  el.setAttribute("aria-modal", "true");
  el.setAttribute("aria-label", "Command bar");
  el.innerHTML = `<div class="cbox"><label class="q"><span class="seal" aria-hidden="true">力</span><span class="pr" aria-hidden="true">›</span><input id="barq" type="text" autocomplete="off" spellcheck="false" placeholder="Find a session or a change" aria-label="Find a session or a change" aria-controls="barlist"><span class="bn" id="barn"></span></label>
    <div id="barlist" class="blist" role="listbox" aria-label="Results"></div>
    <footer><kbd>↑</kbd><kbd>↓</kbd> move <kbd>enter</kbd> open <kbd>esc</kbd> close</footer></div>`;
  document.body.append(el);
  el.addEventListener("mousedown", (e) => { if (e.target === el) closeBar(); });
  const q = $("barq");
  q.addEventListener("input", () => { bar.q = q.value; bar.sel = 0; drawBar(); });
  q.addEventListener("keydown", (e) => {
    // The bar's keys are its own: the page's never see them.
    e.stopPropagation();
    const a = keyAction(e, "", true);
    if (a.do === "back" || a.do === "bar") { e.preventDefault(); return closeBar(); }
    if (e.key === "ArrowDown" || e.key === "ArrowUp") { e.preventDefault(); bar.sel = Math.max(0, Math.min(bar.all.length - 1, bar.sel + (e.key === "ArrowDown" ? 1 : -1))); drawBar(); }
    else if (e.key === "Enter") { e.preventDefault(); pick(bar.sel); }
  });
  q.focus();
  drawBar();
  loadChanges().then(() => { if (bar.open) drawBar(); });
}

function closeBar() {
  bar.open = false;
  $("bar")?.remove();
  bar.back?.focus?.();
}

/** Goes where the bar's row i leads; Open leads to the selected result, else the first. */
function pick(i) {
  const x = bar.all[i];
  if (!x) return;
  const href = x.label === "Open" && !x.kind ? (bar.all[bar.chosen] || x).href : x.href;
  if (!href) return;
  closeBar();
  location.hash = href;
}

function drawBar() {
  const { items, actions } = barResults(bar.q, state.sessions, state.changes || []);
  bar.all = [...items, ...actions];
  bar.sel = Math.min(bar.sel, bar.all.length - 1);
  // The result Open opens: the one selected, else the first.
  bar.chosen = bar.sel < items.length ? bar.sel : 0;
  const row = (x, i) => `<a class="r${i === bar.sel ? " on" : ""}${x.href ? "" : " off"}" role="option" aria-selected="${i === bar.sel}" data-i="${i}" href="${esc(x.href || "#")}">
    <span class="mk" aria-hidden="true">${x.kind === "change" ? "◆" : x.kind === "session" ? "履" : "▸"}</span><span class="tx"><b>${esc(x.label)}</b>${x.sub ? `<small>${esc(x.sub)}</small>` : ""}</span>${i === bar.sel ? "<kbd>enter</kbd>" : ""}</a>`;
  $("barlist").innerHTML = (items.length ? `<div class="kick">changes and sessions</div>${items.map(row).join("")}` : `<p class="muted">${bar.q ? "No session or change holds that text." : "No session yet."}</p>`)
    + `<div class="kick">actions</div>${actions.map((x, i) => row(x, items.length + i)).join("")}`
    + (state.changesError ? `<p class="muted">Changes: could not read them (${esc(state.changesError)}).</p>` : "");
  $("barn").textContent = `${items.length} result(s)`;
  for (const a of $("barlist").querySelectorAll("[data-i]")) a.addEventListener("click", (e) => { e.preventDefault(); pick(Number(a.dataset.i)); });
}

/** The served page's keys (keyAction): lists, pages, the bar, the help, a session's tabs. */
function servedKeys() {
  let pending = "", timer;
  return (e) => {
    if (bar.open) return;
    const typing = Boolean(e.target.closest && e.target.closest("input, textarea, select"));
    const a = keyAction(e, pending, typing);
    pending = a.pending;
    clearTimeout(timer);
    if (pending) timer = setTimeout(() => { pending = ""; }, 1500);
    switch (a.do) {
      case "bar": e.preventDefault(); return openBar();
      case "back": if (typing) return e.target.blur(); return back();
      case "go": return void (location.hash = a.to);
      case "move": e.preventDefault(); return moveRow(a.by);
      case "open": if (openRow()) e.preventDefault(); return;
      case "help": return toggleKeys(false);
      case "search": {
        e.preventDefault();
        const to = searchTarget(location.hash);
        if (location.hash !== to) location.hash = to;
        return void setTimeout(() => $(to === "#/memory" ? "mq" : "q")?.focus(), 0);
      }
      case "tab": case "reaudit": if (state.current) sessionKeys(a); return;
    }
  };
}

async function start() {
  const embedded = document.getElementById("session");
  if (embedded) {
    const { session, view, usage } = JSON.parse(embedded.textContent);
    $("app").classList.add("single");
    document.addEventListener("keydown", (e) => {
      const a = keyAction(e, "", Boolean(e.target.closest && e.target.closest("input, textarea, select")));
      if (a.do === "tab") sessionKeys(a);
      else if (a.do === "help") toggleKeys(true);
      else if (a.do === "back") toggleKeys(true, false);
    });
    renderSession(session, view, "resumen", { embedded: true, usage });
    return;
  }
  document.addEventListener("keydown", servedKeys());
  window.addEventListener("hashchange", route);
  $("detail").innerHTML = skeleton(8);
  await loadSessions();
  route();
  // The background audits keep adding summaries: the list and Inicio follow.
  setInterval(() => { if (state.sessionsError || state.sessions.some((s) => !s.summary)) refreshSessions(); }, 30000);
}

if (typeof document !== "undefined") start();
