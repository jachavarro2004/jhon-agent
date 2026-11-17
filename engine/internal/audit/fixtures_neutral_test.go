package audit

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// neutralOrg is the organization uuid every real audit fixture carries in
// place of the account's own (L466).
const neutralOrg = "00000000-0000-4000-8000-000000000000"

func TestAuditFixturesCarryNoOrganizationUuid(t *testing.T) {
	org := regexp.MustCompile(`"organizationUuid"\s*:\s*"([^"]*)"`)
	root := "../../../test/testdata/audit"
	must(t, filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range org.FindAllSubmatch(b, -1) {
			if string(m[1]) != neutralOrg {
				t.Errorf("%s carries an organizationUuid other than %s", p, neutralOrg)
			}
		}
		return nil
	}))
}
