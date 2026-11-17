import unittest

from orders.summary import summarize


class Summary(unittest.TestCase):
    def test_amounts(self):
        out = summarize([{"status": "paid", "amount": 1000}, {"status": "void", "amount": 50}])
        self.assertEqual(out["by_status"], {"paid": 1000})


if __name__ == "__main__":
    unittest.main()
