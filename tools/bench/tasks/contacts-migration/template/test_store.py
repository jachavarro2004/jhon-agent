import json
import os
import tempfile
import unittest

import store


class StoreTest(unittest.TestCase):
    def write(self, data):
        fd, path = tempfile.mkstemp(suffix=".json")
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(data, f)
        self.addCleanup(os.remove, path)
        return path

    def test_loads_contacts(self):
        path = self.write({"schema": store.SCHEMA, "contacts": [{"id": 1, "name": "Ana Ruiz", "email": "a@x.org"}]})
        self.assertEqual([c["id"] for c in store.load(path)], [1])

    def test_refuses_an_unknown_schema(self):
        with self.assertRaises(ValueError):
            store.load(self.write({"schema": 99, "contacts": []}))


if __name__ == "__main__":
    unittest.main()
