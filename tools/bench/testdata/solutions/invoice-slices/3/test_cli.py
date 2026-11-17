import csv
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data", "invoices.json")


def export(*extra):
    with tempfile.TemporaryDirectory() as d:
        out = os.path.join(d, "out.csv")
        p = subprocess.run([sys.executable, os.path.join(HERE, "cli.py"), "export", DATA, out, *extra], capture_output=True, text=True)
        rows = None
        if os.path.exists(out):
            with open(out, newline="", encoding="utf-8") as f:
                rows = list(csv.reader(f))
        return p, rows


class SinceTest(unittest.TestCase):
    def test_since_is_inclusive(self):
        p, rows = export("--since", "2026-02-28")
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual([r[0] for r in rows[1:]], ["INV-001", "INV-003", "INV-004"])

    def test_a_bad_date_exits_2_and_writes_nothing(self):
        p, rows = export("--since", "2026-02-30")
        self.assertEqual(p.returncode, 2)
        self.assertIn("--since", p.stderr)
        self.assertIsNone(rows)


if __name__ == "__main__":
    unittest.main()
