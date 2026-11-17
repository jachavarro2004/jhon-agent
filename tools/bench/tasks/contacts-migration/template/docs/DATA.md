# Data files

Our data files are JSON objects with a top-level `schema` number. Other tools
(the mailer, the CRM sync) add their own fields to the files and to the
records in them, so every script that rewrites a data file follows these rules:

1. **Keep what you do not know.** Every field you do not change stays as it
   is, at the top level and in every record.
2. **Back up first.** Before the first rewrite, copy the file byte for byte
   to `<file>.bak` next to it.
3. **Write atomically.** Write the new content to a temporary file in the
   same directory, then `os.replace` it over the file. Never leave a
   half-written file.
4. **Be idempotent.** A migration run on a file that is already at its
   target schema changes nothing: not the file and not the backup.
