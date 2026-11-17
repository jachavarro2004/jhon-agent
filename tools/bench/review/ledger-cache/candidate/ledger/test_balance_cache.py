import unittest

from ledger import balance_cache


class Balance(unittest.TestCase):
    def test_cached(self):
        calls = []
        f = lambda a: calls.append(a) or 5
        balance_cache._cache.clear()
        self.assertEqual(balance_cache.balance("acme", f), 5)
        self.assertEqual(balance_cache.balance("acme", f), 5)
        self.assertEqual(calls, ["acme"])


if __name__ == "__main__":
    unittest.main()
