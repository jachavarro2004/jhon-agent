import os
import unittest
from datetime import date

import invoices

DATA = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data", "invoices.json")


class LoadTest(unittest.TestCase):
    def test_load_reads_every_invoice_in_order(self):
        got = invoices.load(DATA)
        self.assertEqual([i.id for i in got], ["INV-001", "INV-002", "INV-003", "INV-004"])
        self.assertEqual(got[1].issued, date(2026, 1, 15))
        self.assertEqual(got[3].customer, "Acme, Inc.")
        self.assertEqual(got[2].amount, "0.125")


if __name__ == "__main__":
    unittest.main()
