"""Message 1's lasting behaviour: the export command, column order, row order, quoting."""

import csv
import json
import os
import tempfile
import unittest

import hidden_export as h


class ExportTest(unittest.TestCase):
    def test_columns_rows_and_quoting(self):
        code, err, rows = h.export()
        self.assertEqual(code, 0, err)
        self.assertIsNotNone(rows)
        header = rows[0]
        self.assertEqual(header[:3], ["id", "customer", "issued"])
        self.assertEqual(len(header), 4)
        self.assertTrue(header[3].startswith("amount"), header)
        self.assertEqual([r[0] for r in rows[1:]], h.ORDER)
        by_id = {r[0]: r for r in rows[1:]}
        self.assertEqual(by_id["INV-004"][1], "Acme, Inc.")
        self.assertEqual(by_id["INV-003"][1], 'Initech "East"')
        self.assertEqual(by_id["INV-002"][2], "2026-01-15")

    def test_export_csv_writes_the_same_file(self):
        import export
        import invoices

        code, err, rows = h.export()
        with tempfile.TemporaryDirectory() as d:
            src, out = os.path.join(d, "in.json"), os.path.join(d, "out.csv")
            with open(src, "w", encoding="utf-8") as f:
                json.dump(h.ROWS, f)
            export.export_csv(invoices.load(src), out)
            with open(out, newline="", encoding="utf-8") as f:
                self.assertEqual(list(csv.reader(f)), rows)


if __name__ == "__main__":
    unittest.main()
