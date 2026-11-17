"""Message 1: refunds take money out; an account is matched exactly, never by prefix."""

import unittest

import ledger

E = [
    {"date": "2026-01-03", "account": "acme-1", "kind": "credit", "amount_cents": 5000},
    {"date": "2026-01-20", "account": "acme-1", "kind": "refund", "amount_cents": 1200},
    {"date": "2026-02-02", "account": "acme-10", "kind": "credit", "amount_cents": 800},
    {"date": "2026-02-14", "account": "acme-1", "kind": "debit", "amount_cents": 300},
    {"date": "2026-11-30", "account": "acme-2", "kind": "refund", "amount_cents": 50},
]


class FixesTest(unittest.TestCase):
    def test_balance(self):
        self.assertEqual(ledger.balance(E, "acme-1"), 3500)
        self.assertEqual(ledger.balance(E, "acme-10"), 800)
        self.assertEqual(ledger.balance(E, "acme-2"), -50)
        self.assertEqual(ledger.balance(E, "acme"), 0)

    def test_monthly_totals(self):
        self.assertEqual(ledger.monthly_totals(E), {"2026-01": 3800, "2026-02": 500, "2026-11": -50})
        self.assertEqual(list(ledger.monthly_totals(E)), ["2026-01", "2026-02", "2026-11"])


if __name__ == "__main__":
    unittest.main()
