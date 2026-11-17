import os
import unittest

from catalog import Product
from importer import import_products, price_cents

DATA = os.path.join(os.path.dirname(os.path.abspath(__file__)), "data", "supplier.csv")


class ImporterTest(unittest.TestCase):
    def test_prices(self):
        self.assertEqual(price_cents("1.234,56"), 123456)
        self.assertEqual(price_cents("7"), 700)
        self.assertEqual(price_cents("1.000"), 100000)
        for bad in ["1,234", "-5,00", "3,5x", ""]:
            self.assertIsNone(price_cents(bad), bad)

    def test_the_supplier_file(self):
        products, rejected = import_products(DATA)
        by_sku = {p.sku: p for p in products}
        self.assertEqual(rejected, 2)
        self.assertEqual(by_sku["AB-1"], Product("AB-1", "Mug XL", 1300, ("kitchen",)))
        self.assertEqual(by_sku["AB-2"], Product("AB-2", "Café tin", 123456, ()))


if __name__ == "__main__":
    unittest.main()
