/**
 * The change detail's actions where they land (S30): d opens the changes
 * overlay (lib/ui/shell-changes-view.ts) on the change's worktree, its files
 * unfolded and the first one's diff shown.
 *
 *   jhon test
 */
import "./isolated-env.ts";
import assert from "node:assert/strict";
import test from "node:test";
import { CHANGE_STATUS, type ChangedFile, type WorktreeChanges } from "../lib/ui/shell-changes.ts";
import { WorktreeChangesView } from "../lib/ui/shell-changes-view.ts";

const file = (path: string): ChangedFile => ({ path, added: 1, deleted: 0, status: CHANGE_STATUS.MODIFIED });
const tree = (root: string, ...paths: string[]): WorktreeChanges => ({ root, model: { files: paths.map(file), added: paths.length, deleted: 0 } });

function overlay() {
  const loaded: string[] = [];
  const view = new WorktreeChangesView([tree("/p/acme", "a.py"), tree("/p/acme-wt/abc", "tasks.md", "views.py")], {
    theme: { fg: (_r, t) => t }, rows: 20,
    loadDiff: async (root, f) => { loaded.push(`${root} ${f.path}`); return "@@ -1 +1 @@\n+x"; },
    onOpen() {}, onClose() {}, onRefresh() {}, requestRender() {},
  });
  return { view, loaded };
}

test("the changes overlay opens on the change's worktree: its files unfolded, its first file's diff shown", () => {
  const { view, loaded } = overlay();
  assert.equal(view.focus("/p/acme-wt/abc"), true);
  const text = view.render(120).join("\n");
  assert.match(text, /tasks\.md/);
  assert.match(text, /views\.py/, "the worktree is unfolded");
  assert.deepEqual(loaded, ["/p/acme-wt/abc tasks.md"], "the first file's diff is the one shown");
  view.dispose();
});

test("a folder inside a worktree lands on that worktree; an unknown one changes nothing", () => {
  const { view, loaded } = overlay();
  assert.equal(view.focus("/p/acme-wt/abc/backend"), true);
  view.render(120);
  assert.deepEqual(loaded, ["/p/acme-wt/abc tasks.md"]);
  const other = overlay();
  assert.equal(other.view.focus("/elsewhere"), false);
  assert.doesNotMatch(other.view.render(120).join("\n"), /views\.py/, "nothing unfolded");
  assert.deepEqual(other.loaded, []);
  view.dispose();
  other.view.dispose();
});

test("the detail's d opens the changes overlay filtered to the change's worktree: the others' files are not listed, after a refresh too (L117)", () => {
	const { view, loaded } = overlay();
	assert.equal(view.focus("/p/acme-wt/abc", true), true);
	let text = view.render(120).join("\n");
	assert.match(text, /views\.py/);
	assert.doesNotMatch(text, /a\.py|acme\b · acme\b/, "only the change's worktree");
	assert.match(text, /Changes · abc/, "the title names the worktree it shows");
	view.update([tree("/p/acme", "a.py", "b.py"), tree("/p/acme-wt/abc", "tasks.md", "views.py")]);
	text = view.render(120).join("\n");
	assert.doesNotMatch(text, /a\.py|b\.py/, "a refresh keeps the filter");
	assert.deepEqual(loaded, ["/p/acme-wt/abc tasks.md"]);
	view.dispose();
});
