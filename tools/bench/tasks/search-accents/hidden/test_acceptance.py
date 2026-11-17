import unittest

from handles import handle_key, register
from search import search


class AcceptanceTest(unittest.TestCase):
    def test_search_ignores_accents_both_ways(self):
        items = ["Café Central", "Cafe Luna", "Crème Brûlée", "Bakery"]
        self.assertEqual(search(items, "cafe"), ["Café Central", "Cafe Luna"])
        self.assertEqual(search(items, "CAFÉ"), ["Café Central", "Cafe Luna"])
        self.assertEqual(search(items, "creme brulee"), ["Crème Brûlée"])

    def test_search_still_ignores_case_and_spacing(self):
        self.assertEqual(search(["Tea  House", "Bakery"], " tea house "), ["Tea  House"])
        self.assertEqual(search(["Bakery"], "pizza"), [])

    def test_handles_still_tell_accents_apart(self):
        self.assertNotEqual(handle_key("José"), handle_key("Jose"))
        self.assertEqual(handle_key(" Ana  RUIZ"), handle_key("ana ruiz"))
        taken = set()
        register("José", taken)
        register("Jose", taken)
        self.assertEqual(len(taken), 2)


if __name__ == "__main__":
    unittest.main()
