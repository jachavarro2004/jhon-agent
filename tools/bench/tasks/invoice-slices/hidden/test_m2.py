"""Message 2: amount, two decimals, half up, where amount_cents was."""

import unittest

import hidden_export as h


class AmountTest(unittest.TestCase):
    def test_amount_has_two_decimals_rounded_half_up(self):
        code, err, rows = h.export()
        self.assertEqual(code, 0, err)
        self.assertEqual(rows[0], ["id", "customer", "issued", "amount"])
        self.assertEqual([r[3] for r in rows[1:]], ["2.68", "7.00", "0.13", "10.01"])


if __name__ == "__main__":
    unittest.main()
