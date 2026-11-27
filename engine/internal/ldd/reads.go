package ldd

// Reads is one reading of a repository shared by several questions about
// it (T268): route-guard asks of one PR which changes the branch carries
// (BranchChanges), the change its name gives (FolderlessChange), and the
// projects whose docs went stale (UndocumentedProjects), each from the same
// git answers, read once. A Reads never outlives its command, so a later
// one sees the repository as it is then; nil reads every answer afresh.
// Use it from one goroutine.
type Reads struct{ g gitReads }

// NewReads is an empty Reads.
func NewReads() *Reads { return &Reads{gitReads{}} }

func (r *Reads) reads() gitReads {
	if r == nil {
		return nil
	}
	return r.g
}

// BranchChanges is BranchChanges, read through r.
func (r *Reads) BranchChanges(dir string) (active, archived []string) {
	return r.reads().branchChanges(dir)
}

// FolderlessChange is FolderlessChange, read through r.
func (r *Reads) FolderlessChange(root string) string {
	name, _ := r.reads().folderless(root)
	return name
}

// UndocumentedProjects is UndocumentedProjects, read through r.
func (r *Reads) UndocumentedProjects(top string) []string {
	return r.reads().undocumentedProjects(top)
}
