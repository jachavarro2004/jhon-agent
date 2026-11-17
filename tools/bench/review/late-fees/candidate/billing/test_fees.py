import unittest

from billing.fees import late_fee


class LateFee(unittest.TestCase):
    def test_balance(self):
        self.assertEqual(late_fee({"amount_cents": 20000, "credits_cents": 9900}, 1), 505)

    def test_cap(self):
        self.assertEqual(late_fee({"amount_cents": 20000, "fee_cap_cents": 700}, 3), 700)


if __name__ == "__main__":
    unittest.main()
