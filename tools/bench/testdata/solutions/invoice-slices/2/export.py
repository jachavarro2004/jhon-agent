"""The invoices export Finance reads: CSV."""

import csv
from decimal import ROUND_HALF_UP, Decimal

COLUMNS = ["id", "customer", "issued", "amount"]


def money(amount):
    """amount, a decimal string, with two decimals rounded half up."""
    return str(Decimal(amount).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))


def export_csv(invoices, path):
    """Writes invoices to path as CSV, sorted by issue date, then id."""
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(COLUMNS)
        for inv in sorted(invoices, key=lambda i: (i.issued, i.id)):
            w.writerow([inv.id, inv.customer, inv.issued.isoformat(), money(inv.amount)])
