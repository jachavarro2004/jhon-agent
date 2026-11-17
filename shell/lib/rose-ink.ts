/**
 * shell/lib/rose-ink.ts — the shell follows the wallpaper (S29, L50).
 *
 * Rose ink has one source, `jhon palette` (engine/internal/palette): this
 * module only asks it to rewrite <agent dir>/themes/rose-ink.json when
 * the desktop palette changes; pi hot-reloads its active theme from there.
 * Each change is said on pi's event bus as `jhon:palette-changed`
 * `{ palette }` (shell/CONTRACT.md), for any extension that dresses
 * something else in it; the shell runs no other program. Rose ink is
 * rewritten only while it is pi's active theme (T15.18): another theme
 * chosen in Customize is never pulled back.
 */

import { execFile, type ExecFileException } from "node:child_process";
import { unwatchFile, watchFile } from "node:fs";

type Run = (bin: string, args: string[], done: (error: ExecFileException | null) => void, env?: NodeJS.ProcessEnv) => void;
type Watch = (path: string, onChange: () => void) => () => void;

const defaultRun: Run = (bin, args, done, env) => { execFile(bin, args, { timeout: 10_000, env }, (error) => done(error)); };
const defaultWatch: Watch = (path, onChange) => {
  const listener = () => onChange();
  watchFile(path, { interval: 2000, persistent: false }, listener);
  return () => unwatchFile(path, listener);
};

/** `jhon palette --write-pi`: rose ink rewritten from the palette. */
function writeRoseInk(o: { jhon: string; agentDir: string; colors: string; run?: Run }, done: (error: ExecFileException | null) => void): void {
  // jhon reads the same palette file the shell watches.
  const env = { ...process.env, JHON_DESKTOP_COLORS: o.colors };
  (o.run ?? defaultRun)(o.jhon, ["palette", "--write-pi", o.agentDir], done, env);
}

/**
 * Customize's switch back to rose ink (T15.18): its file is rewritten first,
 * so it is never stale, then applied; a failed rewrite still applies the
 * file as it stands and says so once.
 */
export function switchToRoseInk(o: { jhon: string; agentDir: string; colors: string; run?: Run; apply: () => string | undefined; notify: (message: string) => void }): void {
  writeRoseInk(o, (error) => {
    if (error) o.notify(`Rose ink could not be refreshed (${error.message}); the existing file is applied.`);
    const refused = o.apply();
    if (refused) o.notify(refused);
  });
}

export const PALETTE_CHANGED_EVENT = "jhon:palette-changed";

/** Write the theme once now and again on every palette change, saying each change; returns the stop. */
export function followPalette(o: { jhon: string; agentDir: string; colors: string; palette: string; emit?: (name: string, data: unknown) => void; active?: () => string | undefined; run?: Run; watch?: Watch; onError?: (e: Error) => void }): () => void {
  const report = (error: ExecFileException | null) => { if (error) o.onError?.(error); };
  const write = () => {
    const active = o.active?.();
    if (active === undefined || active === "rose-ink") writeRoseInk(o, report);
  };
  write();
  return (o.watch ?? defaultWatch)(o.colors, () => {
    write();
    o.emit?.(PALETTE_CHANGED_EVENT, { palette: o.palette });
  });
}
