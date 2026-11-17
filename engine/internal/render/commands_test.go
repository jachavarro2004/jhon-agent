package render

import (
	"os"
	"path/filepath"
	"regexp"
)

// The binary sets Commands from internal/cli, which this package cannot
// import; its tests read the same names from the registrations there.
func init() {
	Commands = func() []string {
		names := []string{"help", "hook"}
		files, _ := filepath.Glob(filepath.Join(repo, "engine", "internal", "cli", "cmd_*.go"))
		for _, f := range files {
			src, _ := os.ReadFile(f)
			for _, m := range registered.FindAllSubmatch(src, -1) {
				names = append(names, string(m[1]))
			}
		}
		return names
	}
}

var registered = regexp.MustCompile(`register\("([a-z-]+)"`)
