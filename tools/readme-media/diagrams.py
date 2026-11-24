# Builds docs/assets/diagrams/*.svg: python3 -I tools/readme-media/diagrams.py docs/assets/diagrams
"""Jhon's four diagrams (the README's): plain SVG in
rose ink, drawn with identity.ts's tokens and faces only, in the dashboard's
build (square corners, hard offset shadows, mono kickers, the 力 seal).

Every diagram is 1280x720 (16:9), so it fills a 1920x1080 frame at 1.5x;
no text is smaller than 14 px (21 px in that frame).

Usage: python3 -I diagrams.py <out dir>     the four SVGs
       python3 -I diagrams.py --tokens      the colours used, as JSON
                                            (a test checks them against rose ink)
"""
import json
import sys
from pathlib import Path
from xml.sax.saxutils import escape

# identity.ts `Colors`: rose ink's semantic tokens (`jhon palette --json`),
# by the names identity.ts gives them; `shadow` is identity's own #000000.
# No other colour is drawn. The refuse / ask / allow verdicts are identity's
# state colours (bad, warn, ok).
C = dict(
    page="#1f1f28", frame="#161618", band="#1b1b20", box="#2a2a37", boxStrong="#363646",
    ink="#e9e0e1", soft="#d2c3c6", faint="#9b8d90", line="#4f4447",
    accent="#e7bbc8", accentFill="#53343f", accentInk="#f2c5d2", accentSoft="#b594a0", frameSoft="#7b6772",
    ok="#98bb6c", bad="#ffb4ab", warn="#e6c384", shadow="#000000",
)
# identity.ts `FONTS`.
DISPLAY = "Fraunces, Georgia, serif"
UI = "'Space Grotesk', sans-serif"
MONO = "'FiraCode Nerd Font Mono', 'CaskaydiaCove Nerd Font Mono', 'Noto Sans CJK JP', monospace"
JP = "'Noto Sans CJK JP', sans-serif"

W, H = 1280, 720
# Type sizes: the smallest is 14 px.
KICK, BODY, CHIP, NAME, TITLE = 14, 15, 14, 21, 30
ADVANCE = 0.6154  # identity.ts ADVANCE: the mono face's advance in em
SHADOW = 4


def kick_ls(size):
    return round(size * 0.12, 2)  # identity.ts kicker(): letter-spacing 0.12 em


class Svg:
    def __init__(self, title, desc):
        self.w, self.h, self.parts = W, H, []
        self.head = (
            f'<svg xmlns="http://www.w3.org/2000/svg" width="{W}" height="{H}" viewBox="0 0 {W} {H}" '
            f'role="img" aria-labelledby="t d">\n<title id="t">{escape(title)}</title>\n<desc id="d">{escape(desc)}</desc>\n'
            '<defs>\n'
            + "".join(
                f'<marker id="{mid}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
                f'<path d="M0,1 L9,5 L0,9 z" fill="{C[col]}"/></marker>\n'
                for mid, col in (("ar", "accent"), ("arf", "soft"), ("arw", "warn"), ("aro", "ok"))
            )
            + '</defs>\n'
        )
        self.rect(0, 0, W, H, C["frame"])

    def add(self, s):
        self.parts.append(s)

    def rect(self, x, y, w, h, fill, stroke=None, sw=1, dash=None):
        st = f' stroke="{stroke}" stroke-width="{sw}"' if stroke else ""
        if dash:
            st += f' stroke-dasharray="{dash}"'
        self.add(f'<rect x="{x:g}" y="{y:g}" width="{w:g}" height="{h:g}" fill="{fill}"{st}/>')

    def text(self, x, y, s, size=BODY, fill=None, font=UI, weight=400, anchor="start", italic=False, ls=None):
        assert size >= 14, f"text under 14 px: {s}"
        fill = fill or C["ink"]
        extra = ' font-style="italic"' if italic else ""
        if ls:
            extra += f' letter-spacing="{ls:g}"'
        self.add(
            f'<text x="{x:g}" y="{y:g}" font-family="{escape(font, {chr(39): "&apos;"})}" font-size="{size:g}" font-weight="{weight}" '
            f'fill="{fill}" text-anchor="{anchor}"{extra}>{escape(s)}</text>'
        )

    def kicker(self, x, y, s, col=None, anchor="start", size=KICK):
        self.text(x, y, s.upper(), size, col or C["accent"], MONO, 600, anchor, ls=kick_ls(size))

    def line(self, x1, y1, x2, y2, stroke=None, sw=2, marker="ar", dash=None):
        stroke = stroke or C["accent"]
        d = f' stroke-dasharray="{dash}"' if dash else ""
        m = f' marker-end="url(#{marker})"' if marker else ""
        self.add(f'<line x1="{x1:g}" y1="{y1:g}" x2="{x2:g}" y2="{y2:g}" stroke="{stroke}" stroke-width="{sw}"{d}{m}/>')

    def path(self, d, stroke=None, sw=2, marker="ar", dash=None):
        stroke = stroke or C["accent"]
        ds = f' stroke-dasharray="{dash}"' if dash else ""
        m = f' marker-end="url(#{marker})"' if marker else ""
        self.add(f'<path d="{d}" fill="none" stroke="{stroke}" stroke-width="{sw}"{ds}{m}/>')

    def box(self, x, y, w, h, fill=None, stroke=None, edge=None, dash=None, shadow=True):
        """A square box: the hard offset shadow, the fill, a hairline and an optional left edge."""
        if shadow:
            self.rect(x + SHADOW, y + SHADOW, w, h, C["shadow"])
        self.rect(x, y, w, h, fill or C["box"], stroke or C["line"], 1, dash)
        if edge:
            self.rect(x, y, 4, h, edge)

    def card(self, x, y, w, h, kicker=None, title=None, lines=(), accent=None, fill=None, line_step=22):
        accent = accent or C["accent"]
        self.box(x, y, w, h, fill, edge=accent)
        ty = y + 28
        if kicker:
            self.kicker(x + 16, ty, kicker, accent)
            ty += 30
        if title:
            self.text(x + 16, ty, title, NAME, C["ink"], DISPLAY, 500)
            ty += 28
        for ln in lines:
            s, col = ln if isinstance(ln, tuple) else (ln, C["soft"])
            self.text(x + 16, ty, s, BODY, col)
            ty += line_step

    def chip(self, x, y, s, col=None, w=None, h=28):
        col = col or C["accent"]
        w = w or round(len(s) * CHIP * ADVANCE + 22)
        self.rect(x, y, w, h, C["page"], col, 1)
        self.text(x + w / 2, y + h / 2 + 5, s, CHIP, col, MONO, 600, "middle")
        return w

    def header(self, kicker, title, sub):
        # identity.ts seal(): the accent fill, 力 in the accent ink, the hard shadow.
        self.rect(32 + SHADOW, 28 + SHADOW, 48, 48, C["shadow"])
        self.rect(32, 28, 48, 48, C["accentFill"])
        self.text(56, 64, "力", 29, C["accentInk"], JP, 700, "middle")
        self.kicker(98, 44, kicker)
        self.text(98, 76, title, TITLE, C["ink"], DISPLAY, 500)
        if sub:
            self.text(self.w - 32, 76, sub, BODY, C["faint"], MONO, 400, "end")
        self.add(f'<line x1="32" y1="98" x2="{self.w - 32}" y2="98" stroke="{C["line"]}" stroke-width="1"/>')

    def save(self, path):
        Path(path).write_text(self.head + "\n".join(self.parts) + "\n</svg>\n", encoding="utf-8")


def ldd_flow(out):
    s = Svg("LDD: a change from its first slice to delivery",
            "A change is built in slices. Each slice gets a recorded failing run (RED), the code, a recorded passing run (GREEN), "
            "and a slice commit that route-guard checks is the tested tree. A light verify, or verify and the Prism facets on the "
            "verified route, comes before delivery, which jhon deliver check and route-guard gate.")
    s.header("LDD · Leapfrog-Driven Development", "A change, slice by slice", "value reaches the default branch proven")
    stages = [
        ("01 · change", "Intent", ["branch · tasks.md", "jhon risk → tier", "route: standard", "  or verified"]),
        ("02 · slice", "One slice", ["## Slice <n>: <name>", "Delivers: <criteria>", "spec delta if needed"]),
        ("03 · recorded run", "RED → GREEN", ["jhon executions run", "  RED before the code", "  --affected <base>", "pytest-testmon narrows"]),
        ("04 · slice commit", "Tested tree", ["commit = tested tree", "its scenarios named", "real-database reach", "(delivery: change)"]),
        ("05 · check", "Verify · Prism", ["light verify (standard)", "verify-report.md +", "Prism facets (high risk)"]),
        ("06 · delivery", "Deliver", ["jhon deliver check", "PR: RED before GREEN", "and a passing verify"]),
    ]
    x, y, w, h, gap = 32, 128, 194, 186, 10
    for i, (k, t, lines) in enumerate(stages):
        cx = x + i * (w + gap)
        acc = C["warn"] if i in (3, 5) else C["accent"]
        s.card(cx, y, w, h, k, t, lines, accent=acc)
        if i < len(stages) - 1:
            s.line(cx + w + 1, y + h / 2, cx + w + gap - 1, y + h / 2, sw=2)
    # loop: next slice
    x2 = x + 1 * (w + gap) + w / 2
    x4 = x + 3 * (w + gap) + w / 2
    s.path(f"M{x4:g},{y + h + 8:g} L{x4:g},{y + h + 52:g} L{x2:g},{y + h + 52:g} L{x2:g},{y + h + 12:g}", C["soft"], 1.6, "arf", "6 5")
    s.text((x2 + x4) / 2, y + h + 42, "next slice: back to its spec or tasks, not over", BODY, C["soft"], UI, 400, "middle")
    # gates row
    gy = 410
    s.kicker(32, gy, "who checks", C["warn"])
    gates = [
        (x + 2 * (w + gap), "audit:", "edit-before-red"),
        (x + 3 * (w + gap), "route-guard", "refuses"),
        (x + 4 * (w + gap), "jhon executions", "verify"),
        (x + 5 * (w + gap), "route-guard", "opens the PR"),
    ]
    for gx, a, b in gates:
        s.rect(gx, gy + 16, w, 58, C["page"], C["warn"], 1)
        s.text(gx + w / 2, gy + 40, a, CHIP, C["warn"], MONO, 600, "middle")
        s.text(gx + w / 2, gy + 62, b, CHIP, C["warn"], MONO, 600, "middle")
        s.line(gx + w / 2, y + h + 6, gx + w / 2, gy + 12, C["line"], 1.5, None, "3 4")
    # record strip
    ry = 528
    s.box(32, ry, 1216, 150, C["page"], shadow=False)
    s.kicker(52, ry + 34, "the record")
    s.text(1228, ry + 34, "the rigor rises with the risk; the record is always", BODY, C["faint"], UI, 400, "end")
    rec = ["execution-record.json", "tasks.md", "spec deltas", "verify-report.md", "review-prism.md", "delivery.json", "Engram: ldd state"]
    cx, cy = 52, ry + 56
    for r in rec:
        wid = round(len(r) * 16 * ADVANCE + 24)
        if cx + wid > 1228:
            cx, cy = 52, cy + 42
        s.rect(cx, cy, wid, 32, C["box"], C["line"], 1)
        s.text(cx + 12, cy + 22, r, 16, C["ink"], MONO)
        cx += wid + 12
    s.save(Path(out) / "ldd-flow.svg")


def agents(out):
    s = Svg("Jhon's agents and their launch context",
            "The orchestrator holds the conversation and launches focused sub-agents: jhon-scout, jhon-builder, jhon-checker, "
            "and the Prism facets with a challenger and an arbiter. Each launch carries the brief and, when found, the decisions "
            "on record from Engram, a code map from CodeGraph, and for jhon-checker and the facets the tests to judge.")
    s.header("one orchestrator · focused agents", "Who works, and what each one is handed", "prose/agents.yaml")
    # orchestrator
    s.card(32, 136, 236, 258, "main session", "Orchestrator", [
        "talks with you", "plans the slices", "decides by risk", "delegates on triggers", ("delegation:", C["ink"]), ("  inline | writer", C["ink"]),
    ], accent=C["accent"], fill=C["accentFill"], line_step=24)
    # launch packet
    px, py, pw, ph = 300, 124, 404, 340
    s.box(px, py, pw, ph, C["page"], C["frameSoft"])
    s.kicker(px + 16, py + 30, "the launch message")
    blocks = [
        ("the brief", "task · acceptance · context", C["soft"]),
        ("## Allowed edit surfaces", "a writer's scope · scope-guard", C["warn"]),
        ("## Decisions on record", "Engram · jhon context --handoff", C["accent"]),
        ("## Code map", "CodeGraph query and callers", C["accentSoft"]),
        ("## Tests to judge", "jhon-checker, Prism · jhon context --judge", C["ok"]),
    ]
    by = py + 46
    for title, sub, col in blocks:
        s.box(px + 14, by, pw - 28, 52, C["box"], shadow=False, edge=col)
        s.text(px + 28, by + 22, title, 15, C["ink"], MONO, 600)
        s.text(px + 28, by + 43, sub, BODY, C["soft"], UI)
        by += 57
    s.line(268 + 4, 265, px - 4, 265)
    # agents column
    ax, aw = 740, 508
    rows = [
        ("jhon-scout", "reads, researches · writes only exploration.md or research.md", C["accentSoft"]),
        ("jhon-builder", "writes the slice inside its edit surfaces", C["warn"]),
        ("jhon-checker", "independent check · writes only its report", C["ok"]),
    ]
    ay = 124
    for name, sub, col in rows:
        s.box(ax, ay, aw, 62, edge=col)
        s.text(ax + 16, ay + 26, name, 19, C["ink"], DISPLAY, 500)
        s.text(ax + 16, ay + 50, sub, 14, C["soft"], UI)
        s.line(px + pw + 4, ay + 31, ax - 6, ay + 31, C["accent"], 1.6)
        ay += 76
    # Prism
    ry = ay
    s.box(ax, ry, aw, 132, edge=C["accent"])
    s.text(ax + 16, ry + 26, "Prism review", 19, C["ink"], DISPLAY, 500)
    s.text(ax + aw - 14, ry + 26, "read-only · inspect-guard", 14, C["faint"], MONO, 400, "end")
    cx = ax + 16
    for facet in ("safety", "failure", "clarity", "behavior"):
        cx += s.chip(cx, ry + 42, facet, C["accent"]) + 8
    cx = ax + 16
    w1 = s.chip(cx, ry + 86, "challenger", C["soft"])
    s.line(cx + w1 + 4, ry + 100, cx + w1 + 26, ry + 100, C["soft"], 1.6, "arf")
    cx += w1 + 30
    w2 = s.chip(cx, ry + 86, "arbiter", C["soft"])
    s.line(cx + w2 + 4, ry + 100, cx + w2 + 26, ry + 100, C["soft"], 1.6, "arf")
    cx += w2 + 30
    s.chip(cx, ry + 86, "review-prism.md", C["warn"])
    s.line(px + pw + 4, ry + 66, ax - 6, ry + 66, C["accent"], 1.6)
    # sources
    sy = 520
    s.kicker(32, sy, "where the context comes from", C["warn"])
    srcs = [
        ("Engram", "decisions, and one state record per change", C["accent"]),
        ("CodeGraph", "symbols, callers, impact of the files named", C["accentSoft"]),
        ("execution record", "tests that turned green by editing only their test", C["ok"]),
    ]
    sx, sw = 32, 395
    for name, sub, col in srcs:
        s.rect(sx, sy + 16, sw, 72, C["page"], C["line"], 1)
        s.rect(sx, sy + 16, sw, 4, col)
        s.text(sx + 16, sy + 48, name, 19, C["ink"], DISPLAY, 500)
        s.text(sx + 16, sy + 74, sub, BODY, C["soft"], UI)
        sx += sw + 15
    s.text(32, 676, "Each block is left out on a failure, a timeout or an empty result; with none, the launch is the brief alone.", BODY, C["faint"], UI)
    s.save(Path(out) / "agents-launch-context.svg")


def guards(out):
    s = Svg("The guards around one tool call",
            "Every tool call of every Jhon agent passes jhon hook pre-tool: inspect, safety, vcs, framework, secrets, scope "
            "and route guards in order. The first refusal answers; otherwise the first ask, which a grant or the project's mode "
            "may pass; otherwise the call is allowed and, with sandbox.mode on, a command runs in the OS sandbox.")
    s.header("jhon hook pre-tool", "Around every tool call", "engine/internal/guard")
    # call
    s.card(32, 150, 196, 160, "an agent calls", "Tool call", ["Bash · Edit · Write", "Read · Grep · Glob", "Agent"], accent=C["accentSoft"])
    s.line(232, 230, 262, 230)
    guards = [
        ("inspect", "Bash", "a reviewer stays read-only"),
        ("safety", "Bash", "destructive: refused · outward: asked"),
        ("vcs", "Edit Write", ".git, grants, writes outside the project"),
        ("framework", "Edit Write Bash", "Jhon's own files and release"),
        ("secrets", "Read Grep Glob Edit Write", "files that hold credentials"),
        ("scope", "Edit Write Bash", "a writer's edit surfaces"),
        ("route", "Edit Write Bash Agent", "LDD: record, tested tree, PR"),
    ]
    gx, gy, gw, gh = 268, 116, 496, 56
    for i, (name, tools, risk) in enumerate(guards):
        y = gy + i * (gh + 8)
        s.box(gx, y, gw, gh, edge=C["accent"])
        s.text(gx + 16, y + 23, f"{name}-guard", 16, C["ink"], MONO, 600)
        s.text(gx + gw - 12, y + 23, tools, 14, C["faint"], MONO, 400, "end")
        s.text(gx + 16, y + 45, risk, BODY, C["soft"], UI)
    s.text(gx, gy + 7 * (gh + 8) + 18, "in this order · each judges only its tools", BODY, C["faint"], UI)
    # outcomes
    ox, ow = 812, 436
    oy = 116
    outs = [
        ("refuse", "the first refusal is the answer, in every mode", C["bad"]),
        ("ask", "the owner answers · a grant or the mode may pass it", C["warn"]),
        ("allow", "every guard's note joined for the agent", C["ok"]),
    ]
    jx, jy = gx + gw + 14, 320
    for i, (k, sub, col) in enumerate(outs):
        y = oy + i * 96
        s.box(ox, y, ow, 76, C["page"], col, edge=col)
        s.text(ox + 20, y + 33, k, 24, col, DISPLAY, 600)
        s.text(ox + 20, y + 59, sub, BODY, C["soft"], UI)
        s.line(jx, jy, ox - 6, y + 38, col, 1.6, None)
    s.line(gx + gw + 4, jy, jx, jy, C["accent"], 1.6, None)
    s.add(f'<circle cx="{jx}" cy="{jy}" r="5" fill="{C["accent"]}"/>')
    # sandbox
    sy = 428
    s.box(ox, sy, ow, 132, C["box"], C["frameSoft"], dash="6 4")
    s.kicker(ox + 20, sy + 30, "sandbox · optional")
    s.text(ox + 20, sy + 58, "sandbox.mode = on: an allowed command runs", BODY, C["ink"], UI)
    s.text(ox + 20, sy + 84, "in jhon sandbox run (bubblewrap, Linux):", BODY, C["ink"], UI)
    s.text(ox + 20, sy + 110, "project writable, credentials hidden", BODY, C["soft"], UI)
    s.line(ox + ow / 2, oy + 2 * 96 + 80, ox + ow / 2, sy - 6, C["ok"], 1.6, "aro")
    # owner levers
    ly = 616
    s.rect(32, ly, 1216, 60, C["page"], C["line"], 1)
    s.kicker(52, ly + 36, "yours", C["warn"])
    s.text(130, ly + 36, "jhon mode normal | yolo | super-yolo  ·  jhon grant <class> --for 30m  ·  agents can never run either", BODY, C["ink"], MONO)
    s.save(Path(out) / "guards-tool-call.svg")


def runtimes(out):
    s = Svg("Two runtimes over one engine",
            "Claude Code (jhon start) and pi (jhon start pi with the jhon-shell package) both run the same jhon binary for "
            "their guards and records. The binary renders prose/ into each runtime from an installed release, and the dashboard "
            "reads only what the binary serves.")
    s.header("one engine · two runtimes", "Same rules, your choice of runtime", "jhon start · jhon start pi")
    # runtimes
    s.card(32, 124, 588, 178, "jhon start", "Claude Code", [
        "the orchestrator agent + the release's plugin",
        "/jhon-ldd · the read-only jhon MCP server",
        "guards in hooks: jhon hook pre-tool",
        "runs Claude",
    ], accent=C["accent"])
    s.card(660, 124, 588, 178, "jhon start pi", "pi + jhon-shell", [
        "orchestrator prompt, sub-agents, guards on tool_call",
        "rail, panels, /jhon:status, /jhon:agents, /jhon:customize",
        "talks to Jhon only through the binary",
        "runs Claude or another provider, per profile",
    ], accent=C["accent"])
    for x in (326, 954):
        s.line(x, 310, x, 346, sw=2)
    # engine
    ey = 352
    s.box(32, ey, 1216, 146, C["accentFill"], C["frameSoft"])
    s.rect(32, ey, 1216, 4, C["accent"])
    s.kicker(52, ey + 34, "the engine · one go binary")
    s.text(52, ey + 70, "jhon", 28, C["ink"], DISPLAY, 500)
    cx = 52
    for p in ["guards", "LDD records", "launch context", "render", "audit", "sandbox", "settings · profiles"]:
        cx += s.chip(cx, ey + 92, p, C["accentInk"]) + 10
    # sides
    s.card(32, 548, 380, 128, "source of the rules", "prose/", ["agents.yaml · contracts · persona · skills", "jhon sync / jhon update → a release"], accent=C["warn"])
    s.card(450, 548, 380, 128, "always on", "Engram · CodeGraph", ["memory and decisions · the code map", "shared by both runtimes"], accent=C["accentSoft"])
    s.card(868, 548, 380, 128, "reads the binary's JSON", "Dashboard", ["jhon dashboard · 127.0.0.1:7480", "sessions, changes, usage, memory"], accent=C["soft"])
    s.line(222, 544, 222, 504, C["warn"], 1.6, "arw")
    s.line(640, 504, 640, 544, C["soft"], 1.6, "arf")
    s.line(1058, 504, 1058, 544, C["soft"], 1.6, "arf")
    s.save(Path(out) / "runtimes-engine.svg")


if __name__ == "__main__":
    if sys.argv[1:] == ["--tokens"]:
        print(json.dumps(C, sort_keys=True))
        sys.exit(0)
    out = Path(sys.argv[1])
    out.mkdir(parents=True, exist_ok=True)
    ldd_flow(out)
    agents(out)
    guards(out)
    runtimes(out)
