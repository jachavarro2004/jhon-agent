// A data migration is high risk (T148): a code or SQL file whose base name
// starts with migrate or migration, any file under a migrations, migration,
// migrate or db/migrate folder at any depth, golang-migrate and Flyway SQL.
// Its reason is the migration's, "migration or schema", naming the file.
// Documents and tests that only mention a migration stay passive, and
// vendored code and lockfiles keep their tier.
package ldd

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

// touched is a change to the repository's test, so code alone is medium.
const touched = "import { one } from '../src/app';\n// touched\n"

func TestRiskDataMigrationIsHigh(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, text string }{
		{"migrate.py", "def migrate(path): pass\n"},
		{"Migrate.py", "def migrate(path): pass\n"},
		{"src/migrations.ts", "export const migrations = [];\n"},
		{"cmd/migrate_users.go", "package main\n"},
		{"scripts/migration.rb", "puts 1\n"},
		{"db/migrate_v2.sql", "update contacts set schema = 2;\n"},
		{"app/migrate/0002_split.py", "def up(): pass\n"},
		{"app/migrate/README", "how to run\n"},
		{"users/migrations/0002_auto.py", "operations = []\n"},
		{"src/migration/001.ts", "export {};\n"},
		{"db/migrate/20240101000000_add_users.rb", "class AddUsers; end\n"},
		{"migrations/versions/abc123_add.py", "def upgrade(): pass\n"},
		{"prisma/migrations/20240101_init/migration.sql", "alter table a drop column b;\n"},
		{"migrations/20240101_users.js", "exports.up = () => {};\n"},
		{"sql/0001_init.up.sql", "alter table a drop column b;\n"},
		{"sql/0001_init.down.sql", "drop table a;\n"},
		{"db/V1__init.sql", "alter table a drop column b;\n"},
		{"db/U1__init.sql", "drop table a;\n"},
	} {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			root := riskRepo(t)
			put(t, root, c.file, c.text)
			put(t, root, "test/app.test.ts", touched)
			r := risk(t, root)
			equal(t, r.Tier, "high", why(r))
			match(t, why(r), `(?m)^high `+regexp.QuoteMeta(c.file)+`: migration or schema$`)
			text := jhonGit(t, root, "risk")
			equal(t, text.Code, 0, text.Stderr)
			match(t, text.Stdout, `(?m)^risk: high \(base HEAD\)$`)
			match(t, text.Stdout, `(?m)^  high +`+regexp.QuoteMeta(c.file)+`: migration or schema$`)
		})
	}
}

func TestRiskMigrationLookalikes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want string }{
		{"docs/migration-guide.md", "passive docs/migration-guide.md: documentation"},
		{"MIGRATING.md", "passive MIGRATING.md: documentation"},
		{"migration-notes.txt", "passive migration-notes.txt: documentation"},
		{"test_migrate.py", "passive test_migrate.py: test"},
		{"migrate_test.go", "passive migrate_test.go: test"},
		{"src/migrate.test.ts", "passive src/migrate.test.ts: test"},
		{"vendor/github.com/x/migrate/migrate.go", "medium vendor/github.com/x/migrate/migrate.go: code"},
		{"node_modules/umzug/lib/migrate.js", "medium node_modules/umzug/lib/migrate.js: code"},
		{"package-lock.json", "medium package-lock.json: other file"},
		{"src/migrator.go", "medium src/migrator.go: code"},
	} {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			root := riskRepo(t)
			put(t, root, c.file, "x\n")
			put(t, root, "test/app.test.ts", touched)
			r := risk(t, root)
			match(t, why(r), `(?m)^`+regexp.QuoteMeta(c.want)+`$`)
			if r.Tier == "high" {
				t.Errorf("%s alone is high:\n%s", c.file, why(r))
			}
		})
	}
}

// The real case (L1295): the bench task contacts-migration adds a root
// migrate.py that moves data/contacts.json from schema 1 to schema 2, makes
// store.py read schema 2, and tests it. It was medium; it is high.
func TestRiskContactsMigration(t *testing.T) {
	t.Parallel()
	root := support.GitRepo(t, temp(t))
	template := support.Path("tools", "bench", "tasks", "contacts-migration", "template")
	err := filepath.WalkDir(template, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(template, p)
		put(t, root, rel, string(b))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	put(t, root, "openspec/config.yaml", "schema: spec-driven\n")
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "contacts-schema-2")
	put(t, root, "migrate.py", "import json\n\n\ndef migrate(path):\n    with open(path, encoding=\"utf-8\") as f:\n        data = json.load(f)\n    for c in data[\"contacts\"]:\n        first, _, last = c.pop(\"name\").partition(\" \")\n        c[\"first_name\"], c[\"last_name\"] = first, last\n    data[\"schema\"] = 2\n    with open(path, \"w\", encoding=\"utf-8\") as f:\n        json.dump(data, f)\n")
	store := support.Read(t, filepath.Join(root, "store.py"))
	put(t, root, "store.py", regexp.MustCompile(`SCHEMA = 1`).ReplaceAllString(store, "SCHEMA = 2"))
	put(t, root, "test_migrate.py", "import unittest\n\nimport migrate\n\n\nclass MigrateTest(unittest.TestCase):\n    def test_splits(self):\n        self.assertTrue(migrate.migrate)\n")
	r := risk(t, root)
	equal(t, r.Tier, "high", why(r))
	equal(t, why(r), "medium store.py: code\nhigh migrate.py: migration or schema\npassive test_migrate.py: test")
	text := jhonGit(t, root, "risk")
	equal(t, text.Code, 0, text.Stderr)
	match(t, text.Stdout, `(?m)^risk: high \(base HEAD\)$`)
	match(t, text.Stdout, `(?m)^  high +migrate\.py: migration or schema$`)
	route, _ := statusJSON(t, root)["route"].(map[string]any)
	equal(t, route["level"], "verified", route)
	equal(t, route["tier"], "high", route)
}
