/**
 * Measuring jhon-shell's UI before pruning it: a transcript records the
 * tools a model calls, not the commands and shortcuts a person runs. Each
 * extension registers through counted(pi), so every run of one of its
 * commands or shortcuts appends a `jhon-ui-use` session entry naming it
 * (`pi.appendEntry`: kept out of the model's context), which
 * `jhon audit --all` counts. A click the shell handles itself is recorded
 * the same way with recordUse (T171): `click agents` or `click jobs` for
 * each OPEN_TASKS_EVENT request (a click on a row of the pane's Agents or Jobs, or of the narrow form's Running block). Counting never
 * breaks the feature. shell/CONTRACT.md,
 * "Session entries".
 */

export const UI_USE_ENTRY = "jhon-ui-use";

type Handler = (...args: any[]) => unknown;
type Registrar = (name: any, def: any) => unknown;
type Recorder = { appendEntry?: (type: string, data: unknown) => void };
type Countable = Recorder & { registerCommand?: Registrar; registerShortcut?: Registrar };

/** Append one `jhon-ui-use` entry naming feature; never throws. */
export function recordUse(pi: Recorder, feature: string): void {
  try { pi.appendEntry?.(UI_USE_ENTRY, { feature }); } catch { /* no session to record in: the feature still runs */ }
}

/** pi with its commands and shortcuts counted; everything else is pi's own. */
export function counted<T extends Countable>(pi: T): T {
  const mark = (feature: string) => recordUse(pi, feature);
  const wrap = (feature: string, def: any) => {
    if (!def || typeof def.handler !== "function") return def;
    const handler: Handler = def.handler;
    return { ...def, handler: (...args: any[]) => { mark(feature); return handler(...args); } };
  };
  return new Proxy(pi, {
    get(target, prop, receiver) {
      if (prop === "registerCommand" && target.registerCommand) {
        return (name: string, def: any) => target.registerCommand!(name, wrap(`/${name}`, def));
      }
      if (prop === "registerShortcut" && target.registerShortcut) {
        return (key: string, def: any) => target.registerShortcut!(key, wrap(`key ${key}`, def));
      }
      const value = Reflect.get(target, prop, receiver);
      // pi's own methods run on pi itself, whatever private state they keep.
      return typeof value === "function" ? value.bind(target) : value;
    },
  });
}
