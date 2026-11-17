import unittest

from pager import paginate


class AcceptanceTest(unittest.TestCase):
    def test_first_page(self):
        self.assertEqual(paginate(list(range(25)), 1, 10), list(range(0, 10)))

    def test_middle_and_last_partial_page(self):
        self.assertEqual(paginate(list(range(25)), 2, 10), list(range(10, 20)))
        self.assertEqual(paginate(list(range(25)), 3, 10), list(range(20, 25)))

    def test_page_past_the_end_is_empty(self):
        self.assertEqual(paginate(list(range(25)), 4, 10), [])


if __name__ == "__main__":
    unittest.main()
