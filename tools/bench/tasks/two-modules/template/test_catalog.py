import unittest

from catalog import Product


class ProductTest(unittest.TestCase):
    def test_a_product_has_no_tags_by_default(self):
        p = Product("AB-1", "Mug", 1250)
        self.assertEqual(p.tags, ())
        self.assertEqual(p, Product("AB-1", "Mug", 1250, ()))


if __name__ == "__main__":
    unittest.main()
