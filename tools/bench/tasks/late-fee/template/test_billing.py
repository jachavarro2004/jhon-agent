import datetime
import unittest

from billing import balance, days_late
from money import format_cents, percent_of

DUE = datetime.date(2026, 1, 1)


class BillingTest(unittest.TestCase):
    def test_balance_is_the_amount_minus_credits(self):
        self.assertEqual(balance({"amount": 5000, "credits": 1200, "due": DUE}), 3800)

    def test_days_late(self):
        inv = {"amount": 1, "credits": 0, "due": DUE}
        self.assertEqual(days_late(inv, DUE), 0)
        self.assertEqual(days_late(inv, datetime.date(2025, 12, 20)), 0)
        self.assertEqual(days_late(inv, datetime.date(2026, 1, 11)), 10)

    def test_money(self):
        self.assertEqual(percent_of(1000, 10), 100)
        self.assertEqual(format_cents(1205), "12.05")


if __name__ == "__main__":
    unittest.main()
