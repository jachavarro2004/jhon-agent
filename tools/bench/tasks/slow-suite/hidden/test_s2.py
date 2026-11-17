"""Message 2: a voided entry counts nowhere."""

import unittest

import ledger

E = [
    {"date": "2026-01-03", "account": "acme-1", "kind": "credit", "amount_cents": 5000},
    {"date": "2026-01-04", "account": "acme-1", "kind": "debit", "amount_cents": 700, "voided": True},
    {"date": "2026-03-09", "account": "acme-1", "kind": "credit", "amount_cents": 900, "voided": True},
    {"date": "2026-03-10", "account": "acme-2", "kind": "credit", "amount_cents": 60, "voided": False},
]


class VoidedTest(unittest.TestCase):
    def test_balance_skips_voided(self):
        self.assertEqual(ledger.balance(E, "acme-1"), 5000)
        self.assertEqual(ledger.balance(E, "acme-2"), 60)

    def test_monthly_totals_skip_voided(self):
        self.assertEqual(ledger.monthly_totals(E), {"2026-01": 5000, "2026-03": 60})


if __name__ == "__main__":
    unittest.main()
