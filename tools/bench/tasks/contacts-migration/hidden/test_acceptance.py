import json
import os
import shutil
import tempfile
import unittest

import store
from migrate import migrate

ORIGINAL = {
    "schema": 1,
    "synced_at": "2026-10-01T08:00:00Z",
    "contacts": [
        {"id": 1, "name": "Ana María Ruiz", "email": "ana@example.com", "crm_id": "C-17"},
        {"id": 2, "name": "Bo", "email": "bo@example.com", "tags": ["vip"]},
    ],
}


class AcceptanceTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.addCleanup(shutil.rmtree, self.dir)
        self.path = os.path.join(self.dir, "contacts.json")
        with open(self.path, "w", encoding="utf-8") as f:
            json.dump(ORIGINAL, f, indent=2)
        with open(self.path, "rb") as f:
            self.before = f.read()

    def read(self, path):
        with open(path, "rb") as f:
            return f.read()

    def test_splits_names_and_keeps_every_other_field(self):
        migrate(self.path)
        data = json.loads(self.read(self.path))
        self.assertEqual(data["schema"], 2)
        self.assertEqual(data["synced_at"], "2026-10-01T08:00:00Z")
        ana, bo = data["contacts"]
        self.assertEqual((ana["first_name"], ana["last_name"]), ("Ana", "María Ruiz"))
        self.assertEqual((bo["first_name"], bo["last_name"]), ("Bo", ""))
        self.assertNotIn("name", ana)
        self.assertEqual((ana["id"], ana["email"], ana["crm_id"]), (1, "ana@example.com", "C-17"))
        self.assertEqual((bo["id"], bo["email"], bo["tags"]), (2, "bo@example.com", ["vip"]))

    def test_backs_up_the_original_and_leaves_no_temporary_file(self):
        migrate(self.path)
        self.assertEqual(self.read(self.path + ".bak"), self.before)
        self.assertEqual(sorted(os.listdir(self.dir)), ["contacts.json", "contacts.json.bak"])

    def test_a_second_run_changes_nothing(self):
        migrate(self.path)
        after, backup = self.read(self.path), self.read(self.path + ".bak")
        migrate(self.path)
        self.assertEqual(self.read(self.path), after)
        self.assertEqual(self.read(self.path + ".bak"), backup)

    def test_the_store_reads_the_migrated_file(self):
        migrate(self.path)
        contacts = store.load(self.path)
        self.assertEqual([c["id"] for c in contacts], [1, 2])


if __name__ == "__main__":
    unittest.main()
