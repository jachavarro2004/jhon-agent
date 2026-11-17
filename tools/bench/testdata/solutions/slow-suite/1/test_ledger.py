import unittest

import ledger

ENTRIES = [
    {"date": "2026-03-01", "account": "acme-1", "kind": "credit", "amount_cents": 10000},
    {"date": "2026-03-05", "account": "acme-1", "kind": "debit", "amount_cents": 2500},
    {"date": "2026-04-02", "account": "acme-2", "kind": "credit", "amount_cents": 700},
]


class LedgerTest(unittest.TestCase):
    def test_balance_is_credits_minus_debits(self):
        self.assertEqual(ledger.balance(ENTRIES, "acme-1"), 7500)
        self.assertEqual(ledger.balance(ENTRIES, "acme-2"), 700)

    def test_monthly_totals_by_month_in_order(self):
        self.assertEqual(ledger.monthly_totals(ENTRIES), {"2026-03": 7500, "2026-04": 700})

    def test_a_refund_takes_money_out(self):
        entries = ENTRIES + [{"date": "2026-04-09", "account": "acme-2", "kind": "refund", "amount_cents": 200}]
        self.assertEqual(ledger.balance(entries, "acme-2"), 500)
        self.assertEqual(ledger.monthly_totals(entries)["2026-04"], 500)

    def test_an_account_is_not_another_with_its_prefix(self):
        entries = ENTRIES + [{"date": "2026-03-02", "account": "acme-10", "kind": "credit", "amount_cents": 99}]
        self.assertEqual(ledger.balance(entries, "acme-1"), 7500)
        self.assertEqual(ledger.balance(entries, "acme-10"), 99)


if __name__ == "__main__":
    unittest.main()
