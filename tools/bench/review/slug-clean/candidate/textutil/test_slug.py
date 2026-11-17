import unittest

from textutil.slug import slugify


class Slugify(unittest.TestCase):
    def test_words(self):
        self.assertEqual(slugify("  Café   con Leche! "), "cafe-con-leche")

    def test_length(self):
        self.assertEqual(slugify("ab cd", max_length=3), "ab")

    def test_empty(self):
        self.assertEqual(slugify("¡¿?!"), "")

    def test_bad_length(self):
        with self.assertRaises(ValueError):
            slugify("x", max_length=0)


if __name__ == "__main__":
    unittest.main()
