import unittest

from pager import page_count, paginate


class PageCountTest(unittest.TestCase):
    def test_counts_partial_pages(self):
        self.assertEqual(page_count(25, 10), 3)
        self.assertEqual(page_count(20, 10), 2)
        self.assertEqual(page_count(0, 10), 0)

    def test_rejects_a_size_below_one(self):
        with self.assertRaises(ValueError):
            page_count(5, 0)


class PaginateTest(unittest.TestCase):
    def test_rejects_page_zero(self):
        with self.assertRaises(ValueError):
            paginate([1, 2, 3], 0, 2)

    def test_rejects_a_size_below_one(self):
        with self.assertRaises(ValueError):
            paginate([1, 2, 3], 1, 0)


if __name__ == "__main__":
    unittest.main()
