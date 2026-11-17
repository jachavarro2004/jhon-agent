import datetime
import unittest

from billing import late_fee

DUE = datetime.date(2026, 1, 1)


def day(n):
    return DUE + datetime.timedelta(days=n)


class AcceptanceTest(unittest.TestCase):
    def test_no_fee_up_to_30_days_late(self):
        inv = {"amount": 10000, "credits": 0, "due": DUE}
        self.assertEqual(late_fee(inv, day(-5)), 0)
        self.assertEqual(late_fee(inv, DUE), 0)
        self.assertEqual(late_fee(inv, day(30)), 0)

    def test_five_percent_from_31_days_late(self):
        inv = {"amount": 10000, "credits": 0, "due": DUE}
        self.assertEqual(late_fee(inv, day(31)), 500)
        self.assertEqual(late_fee(inv, day(200)), 500)

    def test_the_fee_is_on_the_balance(self):
        inv = {"amount": 20000, "credits": 9900, "due": DUE}
        self.assertEqual(late_fee(inv, day(31)), 505)

    def test_half_a_cent_rounds_up(self):
        self.assertEqual(late_fee({"amount": 1010, "credits": 0, "due": DUE}, day(31)), 51)
        self.assertEqual(late_fee({"amount": 1050, "credits": 0, "due": DUE}, day(31)), 53)
        self.assertIsInstance(late_fee({"amount": 1010, "credits": 0, "due": DUE}, day(31)), int)


if __name__ == "__main__":
    unittest.main()
