package ldd

import "testing"

// The patterns of additiveMigration, one statement at a time (T164).
func TestAdditiveMigration(t *testing.T) {
	for _, c := range []struct {
		file, text string
		want       bool
	}{
		{"a.sql", "create table a (id int);", true},
		{"a.sql", "CREATE TABLE IF NOT EXISTS public.a (id int, b text NOT NULL)", true},
		{"a.sql", "create table a as select * from b;", false},
		{"a.sql", "create table a (id int) as select 1;", false},
		{"a.sql", "create index on a (b);", true},
		{"a.sql", "create unique index concurrently if not exists i on a (b);", true},
		{"a.sql", "alter table a add column b text;", true},
		{"a.sql", "alter table a add b text default 'x, y' not null;", true},
		{"a.sql", "alter table only a add column if not exists b numeric(10, 2);", true},
		{"a.sql", "alter table a add column b text not null;", false},
		{"a.sql", "alter table a add constraint c check (b > 0);", false},
		{"a.sql", "alter table a add primary key (id);", false},
		{"a.sql", "alter table a add column b text, add column c text;", false},
		{"a.sql", "alter table a drop column b;", false},
		{"a.sql", "alter table a rename to b;", false},
		{"a.sql", "alter table a alter column b type bigint;", false},
		{"a.sql", "alter table a alter column b set not null;", false},
		{"a.sql", "update a set b = 1;", false},
		{"a.sql", "insert into a select * from b;", false},
		{"a.sql", "delete from a;", false},
		{"a.sql", "truncate a;", false},
		{"a.sql", "drop index i;", false},
		{"a.sql", "begin;\n-- c\n/* d\n e */\ncreate table a (id int);\ncommit;\n", true},
		{"a.sql", "begin; commit;", false},
		{"a.sql", "-- migrate:up\ncreate table a (id int);\n-- migrate:down\ndrop table a;\n", true},
		{"a.sql", "-- +migrate Down\ndrop table a;\n-- +migrate Up\ncreate table a (id int);\n", false},
		{"a.rb", "create_table :a do |t|\n  t.string :b\nend\n", true},
		{"a.rb", "create_table :a do |t|\n  t.remove :b\nend\n", false},
		{"a.rb", "create_table :a do |t|\n  t.string :b\n", false},
		{"a.rb", "add_column :a, :b, :string, null: false, default: \"\"", true},
		{"a.rb", "add_column :a, :b, :string, :null => false", false},
		{"a.rb", "add_reference :a, :b, null: false", false},
		{"a.rb", "add_index :a, :b", true},
		{"a.rb", "remove_index :a, :b", false},
		{"a.rb", "rename_table :a, :b", false},
		{"a.rb", "change_column_null :a, :b, false", false},
		{"a.rb", "def change\n  add_column :a, :b, :string\n  reversible do |dir|\n  end\nend\n", false},
		{"a.rb", "def change\n  add_column :a, :b, :string\n  execute \"UPDATE a SET b = 1\"\nend\n", false},
		{"a.py", "migrations.AddField(model_name='a', name='b', field=models.IntegerField(null=True)),", true},
		{"a.py", "migrations.AddField(model_name='a', name='b', field=models.IntegerField()),", false},
		{"a.py", "migrations.RenameField(model_name='a', old_name='b', new_name='c'),", false},
		{"a.py", "migrations.DeleteModel(name='A'),", false},
		{"a.py", "migrations.RunSQL('DROP TABLE a'),", false},
		{"a.py", "op.add_column('a', sa.Column('b', sa.Integer()))", false},
		{"a.py", "op.create_index('i', 'a', ['b'])\nop.drop_table('c')", false},
		{"a.py", "op.create_table('a', sa.Column('id', sa.Integer()))  # a table", true},
		{"a.py", "revision = make_revision()\nop.create_index('i', 'a', ['b'])", false},
		{"a.py", "import json\nop.create_index('i', 'a', ['b'])\nprint(json.dumps({}))", false},
		{"a.py", "def upgrade():\n    pass\n", false},
		{"a.js", "exports.up = () => {};", false},
		{"schema.prisma", "model A { id Int @id }", false},
		{"a.sql", "", false},
		// L1427: what read as additive and can lose data.
		{"a.rb", "create_table :a, force: :cascade do |t|\n  t.string :b\nend\n", false},
		{"a.rb", "create_table :a, :force => true do |t|\n  t.string :b\nend\n", false},
		{"a.rb", "create_table :a, as: \"SELECT 1\"", false},
		{"a.rb", "create_table :a, id: :uuid, comment: \"x\" do |t|\n  t.string :b\nend\n", true},
		{"a.rb", "add_column :a, :b, :string; remove_column :a, :c", false},
		{"a.rb", "add_index :a, :b; drop_table :c", false},
		{"a.rb", "add_index :a, :b if drop_table(:c)", false},
		{"a.rb", "add_column(:a, :b, :string) && remove_column(:a, :c)", false},
		{"a.rb", "create_table :a do |t|\n  t.string :b; drop_table :c\nend\n", false},
		{"a.rb", "add_column :a, :b, :string, null: false, default: nil", false},
		{"a.rb", "add_column :a, :b, :string, null: false, comment: \"default: 1\"", false},
		{"a.rb", "add_column(:a, :b, :string, default: \"x\")", true},
		{"a.rb", "add_reference :a, :b, foreign_key: { to_table: :c }, index: { unique: true }", true},
		{"a.py", "op.add_column('a', sa.Column('b', sa.Integer(), nullable=True)); op.drop_column('a', 'c')", false},
		{"a.py", "migrations.AddField(model_name='a', name='b', field=models.IntegerField(null=True)), migrations.RemoveField(model_name='a', name='c'),", false},
		{"a.py", "migrations.CreateModel(name='T', fields=[('id', models.AutoField(primary_key=True))]), migrations.DeleteModel(name='Old'),", false},
		{"a.py", "migrations.AddField(model_name='a', name='b)', field=models.IntegerField(null=True)),", true},
		{"a.sql", "alter table a add column b text default '--'; update a set b = '-';", false},
		{"a.sql", "alter table a add column b text default '/*'; drop table c; -- */", false},
		{"a.sql", "alter table a add column b text default 'x\\'; drop table c; --';", false},
		{"a.sql", "create table a (id int); /* never closed", false},
		{"a.sql", "alter table a add column b text default '--'; -- c\n", true},
		{"a.sql", "alter table a add column \"default\" int not null;", false},
		{"a.sql", "alter table a add column b text collate \"default\" not null;", false},
		{"a.sql", "alter table a add column b int not null default null;", false},
		{"a.sql", "create table a (id int);\n-- migrate:down is below\ndrop table b;\n", false},
		{"a.sql", "create table a (id int);\n-- +goose Down\ndrop table b;\n", false},
		{"a.sql", "-- +goose Up\ncreate table a (id int);\n-- +goose Down\ndrop table a;\n", true},
	} {
		if got := additiveMigration(c.file, []byte(c.text), true); got != c.want {
			t.Errorf("additiveMigration(%q, %q) = %v, want %v", c.file, c.text, got, c.want)
		}
	}
	if additiveMigration("a.sql", []byte("create table a (id int);"), false) {
		t.Error("a file that cannot be read is additive")
	}
	for _, b := range []string{"create table a (id int);\x00", "create table \xff (id int);"} {
		if additiveMigration("a.sql", []byte(b), true) {
			t.Errorf("binary or not UTF-8 %q is additive", b)
		}
	}
}
