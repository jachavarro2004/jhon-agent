"""Message 3: --since, inclusive; a bad date exits 2 and writes nothing."""

import unittest

import hidden_export as h


class SinceTest(unittest.TestCase):
    def test_since_keeps_invoices_issued_on_or_after_the_date(self):
        code, err, rows = h.export("--since", "2026-02-28")
        self.assertEqual(code, 0, err)
        self.assertEqual([r[0] for r in rows[1:]], ["INV-001", "INV-003", "INV-004"])

    def test_without_since_every_invoice(self):
        code, err, rows = h.export()
        self.assertEqual(code, 0, err)
        self.assertEqual(len(rows), 5)

    def test_a_bad_date_exits_2_and_writes_no_file(self):
        for bad in ["2026-02-30", "28/02/2026", "soon"]:
            code, err, rows = h.export("--since", bad)
            self.assertEqual(code, 2, bad)
            self.assertTrue(err.strip(), bad)
            self.assertIsNone(rows, bad)


if __name__ == "__main__":
    unittest.main()
