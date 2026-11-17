import unittest

from shipping import ZONES, format_price


class FormatPriceTest(unittest.TestCase):
    def test_formats_cents(self):
        self.assertEqual(format_price(1250), "12.50")
        self.assertEqual(format_price(5), "0.05")

    def test_zones_hold_the_base_prices(self):
        self.assertEqual(ZONES, {"local": 500, "national": 1200})


if __name__ == "__main__":
    unittest.main()
