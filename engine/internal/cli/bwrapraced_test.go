package cli

import "testing"

// T250 (post-rebase TestT248V2RaceTheProject): bubblewrap's own refusals of
// a folder that changed while it mounted it are read as a race, so the run
// ends refused with the race note rather than as an unexplained failure; an
// ordinary bubblewrap error is not.
func TestBwrapRacedReadsEveryRefusalOfAFolderThatMoved(t *testing.T) {
	for _, line := range []string{
		"bwrap: Race condition binding dirfd",
		"bwrap: Can't mount on symlink destination /newroot/p/.claude: Invalid argument",
		"bwrap: Can't bind mount /oldroot/p on /newroot/p: Invalid argument",
		"bwrap: Can't create file at /newroot/p/x: No such file or directory",
		`bwrap: Can't remount readonly on B/home/u/work/sub/victim/.claude: mount_setattr() failed at "/newrootB/home/u/work/sub/victim/.claude": Invalid argument`,
	} {
		if !bwrapRaced(line + "\nmore output") {
			t.Errorf("not read as a race: %q", line)
		}
	}
	for _, line := range []string{
		"bwrap: setting up uid map: Permission denied",
		"bwrap: No permissions to creating new namespace",
		"ordinary output",
	} {
		if bwrapRaced(line) {
			t.Errorf("read as a race: %q", line)
		}
	}
}
