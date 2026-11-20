// A migration the allow-list of T164 reads as additive only when nothing in
// it can lose data (L1427): create_table with force: (it drops the table
// first), a destructive statement after an additive one on the same line
// (Ruby or SQL `;`, Alembic `;`, Django `), migrations.X(`), a `--` inside a
// string that hid the rest of the SQL line, a quoted column named default,
// Rails default: nil, and a dbmate or goose down marker in a file without
// its up marker keep the migration high, "migration or schema".
package ldd

import (
	"regexp"
	"testing"

	"github.com/jachavarro2004/jhon-agent/test/support"
)

var destructiveLookalikes = []struct{ name, file, text string }{
	{"rails force", "db/migrate/20240105_force.rb", "class Force < ActiveRecord::Migration[7.1]\n  def change\n    create_table :users, force: :cascade do |t|\n      t.string :name\n    end\n  end\nend\n"},
	{"rails add_column; remove_column", "db/migrate/20240106_semi.rb", "class Semi < ActiveRecord::Migration[7.1]\n  def change\n    add_column :users, :x, :string; remove_column :users, :name\n  end\nend\n"},
	{"rails add_index; drop_table", "db/migrate/20240107_semi2.rb", "class Semi2 < ActiveRecord::Migration[7.1]\n  def change\n    add_index :users, :x; drop_table :old\n  end\nend\n"},
	{"alembic add_column; drop_column", "alembic/versions/ghi.py", "from alembic import op\nimport sqlalchemy as sa\n\nrevision = \"ghi\"\ndown_revision = \"def\"\n\n\ndef upgrade():\n    op.add_column(\"users\", sa.Column(\"x\", sa.Integer(), nullable=True)); op.drop_column(\"users\", \"name\")\n"},
	{"django AddField, RemoveField", "app/migrations/0006_two.py", "from django.db import migrations, models\n\n\nclass Migration(migrations.Migration):\n    dependencies = [(\"app\", \"0005\")]\n    operations = [\n        migrations.AddField(model_name=\"c\", name=\"x\", field=models.IntegerField(null=True)), migrations.RemoveField(model_name=\"c\", name=\"y\"),\n    ]\n"},
	{"django CreateModel, DeleteModel", "app/migrations/0010_cs.py", "from django.db import migrations, models\n\n\nclass Migration(migrations.Migration):\n    dependencies = []\n    operations = [\n        migrations.CreateModel(name=\"T\", fields=[(\"id\", models.AutoField(primary_key=True))]), migrations.DeleteModel(name=\"Old\"),\n    ]\n"},
	{"sql -- in a string", "db/migrations/130.sql", "ALTER TABLE users ADD COLUMN sep text DEFAULT '--'; UPDATE users SET sep = '-';\n"},
	{"sql column named default", "db/migrations/131.sql", "ALTER TABLE users ADD COLUMN \"default\" int NOT NULL;\n"},
	{"rails default: nil", "db/migrate/20240108_nil.rb", "class Nil < ActiveRecord::Migration[7.1]\n  def change\n    add_column :users, :x, :string, null: false, default: nil\n  end\nend\n"},
	{"migrate:down without migrate:up", "db/migrations/132.sql", "CREATE TABLE a (id int);\n-- migrate:down is below\nDROP TABLE users;\n"},
	{"goose Down without goose Up", "db/migrations/133.sql", "CREATE TABLE a (id int);\n-- +goose Down\nDROP TABLE users;\n"},
}

func TestRiskDestructiveLookalikeStaysHigh(t *testing.T) {
	t.Parallel()
	for _, c := range destructiveLookalikes {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			tier, line, text := riskOfMigration(t, c.file, c.text)
			equal(t, line, "high "+c.file+": migration or schema")
			equal(t, tier, "high", line)
			match(t, text, `(?m)^risk: high \(base HEAD\)$`)
			match(t, text, `(?m)^  high +`+regexp.QuoteMeta(c.file)+`: migration or schema$`)
		})
	}
}

// The force case takes the verified route, with the risk facet.
func TestRiskForceMigrationRoute(t *testing.T) {
	t.Parallel()
	c := destructiveLookalikes[0]
	root := support.GitRepo(t, temp(t))
	support.Write(t, root, map[string]string{
		"src/app.ts":           "export const one = 1;\n",
		"test/app.test.ts":     "import { one } from '../src/app';\n",
		"openspec/config.yaml": "schema: spec-driven\n",
	})
	support.Git(t, root, "add", "-A")
	support.Git(t, root, "commit", "-q", "-m", "base")
	support.Git(t, root, "checkout", "-q", "-b", "force-users")
	put(t, root, c.file, c.text)
	put(t, root, "test/app.test.ts", touched)
	r := risk(t, root)
	equal(t, r.Tier, "high", why(r))
	match(t, why(r), `(?m)^high `+regexp.QuoteMeta(c.file)+`: migration or schema$`)
	route, _ := statusJSON(t, root)["route"].(map[string]any)
	equal(t, route["level"], "verified", route)
	equal(t, route["tier"], "high", route)
	equal(t, route["facets"], []any{"safety"}, route)
}
