package ldd

import (
	"path/filepath"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// What status suggests follows what the change needs (T31b, L328 advisories
// 1, 2, 5 and 7): an unneeded proposal is never next, a folder change is not
// archived before its light verify passes, a folderless change lists no
// folder artifacts, and a light verify passes only a RED before GREEN.

// folderChange is smallRepo with a folder for change c1 holding files.
func folderChange(t *testing.T, files map[string]string) string {
	t.Helper()
	root := smallRepo(t)
	support.Write(t, root, files)
	return root
}

// A partial proposal.md the change does not need is not suggested.
func TestPartialProposalNotNeededIsNotNext(t *testing.T) {
	t.Parallel()
	root := folderChange(t, map[string]string{"openspec/changes/c1/proposal.md": "## Why\n"})
	s := statusJSON(t, root, "c1")
	equal(t, s["needs"].(map[string]any)["proposal"], false, s["needs"])
	equal(t, s["next"], "apply", s["notes"])
}

// A folder change whose light verify is due is verified before it is
// archived; a failing light verify, or one of another tree, keeps it at
// verify.
func TestFolderChangeVerifiesBeforeArchive(t *testing.T) {
	t.Parallel()
	root := folderChange(t, map[string]string{"openspec/changes/c1/tasks.md": "## Intent\nDelivers: B is 1\n\n- [x] B is 1\n"})
	redGreenRuns(t, root, "c1")
	equal(t, statusJSON(t, root, "c1")["next"], "verify", "light verify due")
	equal(t, jhonGit(t, root, "executions", "verify", "c1", "--verdict", "fail").Code, 0)
	equal(t, statusJSON(t, root, "c1")["next"], "verify", "a failing light verify")
	equal(t, jhonGit(t, root, "executions", "verify", "c1", "--verdict", "pass").Code, 0)
	equal(t, statusJSON(t, root, "c1")["next"], "verify", "a pass of HEAD's tree, not the tested one")
	commit(t, root, "slice")
	equal(t, jhonGit(t, root, "executions", "run", "c1").Code, 0, "GREEN on the slice commit")
	equal(t, statusJSON(t, root, "c1")["next"], "verify", "a newer GREEN")
	equal(t, jhonGit(t, root, "executions", "verify", "c1", "--verdict", "pass").Code, 0)
	equal(t, statusJSON(t, root, "c1")["next"], "archive", "a passing light verify of the tested tree")
}

// A folderless change lists no artifacts: it has no folder to hold them.
func TestFolderlessListsNoArtifacts(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	raw := []byte(jhonGit(t, root, "status", smallChange, "--json").Stdout)
	equal(t, string(field(t, raw, "artifacts")), "[]")
}

// A passing light verify of a change with no RED before GREEN is refused,
// and writes nothing; a failing one is recorded.
func TestExecutionsVerifyPassNeedsRedBeforeGreen(t *testing.T) {
	t.Parallel()
	root := smallRepo(t)
	equal(t, jhonGit(t, root, "executions", "run", smallChange).Code, 0, "GREEN without RED")
	before := support.Read(t, filepath.Join(root, smallRecord))
	r := jhonGit(t, root, "executions", "verify", smallChange, "--verdict", "pass")
	equal(t, r.Code, 2, r.Stderr)
	equal(t, r.Stdout, "")
	equal(t, r.Stderr, "jhon executions: no RED before GREEN on record for "+smallChange+": a light verify passes a change whose new test failed first; run `jhon executions run "+smallChange+" --affected <base>` while its new test fails (RED), a unit's run (`--affected`) between, and `--affected <base>` again once it passes (GREEN), where <base> is the commit the slice started from (with none, a full `jhon executions run "+smallChange+"`)\n")
	equal(t, support.Read(t, filepath.Join(root, smallRecord)), before, "nothing written")
	equal(t, jhonGit(t, root, "executions", "verify", smallChange, "--verdict", "fail").Code, 0, "a fail is recorded")
	equal(t, len(lightVerifyNotes(t, filepath.Join(root, smallRecord))), 1)
}
