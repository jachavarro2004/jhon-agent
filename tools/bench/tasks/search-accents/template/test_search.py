import unittest

from search import search


class SearchTest(unittest.TestCase):
    def test_ignores_case_and_spacing(self):
        items = ["Café Central", "Tea  House", "Bakery"]
        self.assertEqual(search(items, "tea house"), ["Tea  House"])
        self.assertEqual(search(items, "BAK"), ["Bakery"])

    def test_no_match_is_empty(self):
        self.assertEqual(search(["Bakery"], "pizza"), [])


if __name__ == "__main__":
    unittest.main()
