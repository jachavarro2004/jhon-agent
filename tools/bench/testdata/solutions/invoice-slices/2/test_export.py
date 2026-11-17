import csv
import os
import subprocess
import sys
import tempfile
import unittest

import export
import invoices

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data", "invoices.json")


def read(path):
    with open(path, newline="", encoding="utf-8") as f:
        return list(csv.reader(f))


class ExportTest(unittest.TestCase):
    def test_columns_order_and_half_up_amount(self):
        with tempfile.TemporaryDirectory() as d:
            out = os.path.join(d, "out.csv")
            export.export_csv(invoices.load(DATA), out)
            rows = read(out)
        self.assertEqual(rows[0], ["id", "customer", "issued", "amount"])
        self.assertEqual([r[0] for r in rows[1:]], ["INV-002", "INV-001", "INV-003", "INV-004"])
        self.assertEqual([r[3] for r in rows[1:]], ["2.68", "7.00", "0.13", "10.01"])
        self.assertEqual(rows[4][1], "Acme, Inc.")

    def test_the_export_command(self):
        with tempfile.TemporaryDirectory() as d:
            out = os.path.join(d, "out.csv")
            p = subprocess.run([sys.executable, os.path.join(HERE, "cli.py"), "export", DATA, out], capture_output=True, text=True)
            self.assertEqual(p.returncode, 0, p.stderr)
            self.assertEqual(len(read(out)), 5)


if __name__ == "__main__":
    unittest.main()
