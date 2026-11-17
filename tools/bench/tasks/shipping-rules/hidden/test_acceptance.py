import unittest

from shipping import shipping_cost


class AcceptanceTest(unittest.TestCase):
    def test_base_price_by_zone(self):
        self.assertEqual(shipping_cost(1, "local", False), 500)
        self.assertEqual(shipping_cost(2, "national", False), 1200)

    def test_each_started_kg_above_two_adds_150(self):
        self.assertEqual(shipping_cost(2.1, "local", False), 650)
        self.assertEqual(shipping_cost(3, "local", False), 650)
        self.assertEqual(shipping_cost(3.5, "local", False), 800)

    def test_express_doubles_the_whole_price(self):
        self.assertEqual(shipping_cost(3.5, "national", True), 3000)
        self.assertEqual(shipping_cost(1, "local", True), 1000)

    def test_rejects_an_unknown_zone_and_a_weight_of_zero(self):
        with self.assertRaises(ValueError):
            shipping_cost(1, "abroad", False)
        with self.assertRaises(ValueError):
            shipping_cost(0, "local", False)
        with self.assertRaises(ValueError):
            shipping_cost(-1, "local", False)


if __name__ == "__main__":
    unittest.main()
