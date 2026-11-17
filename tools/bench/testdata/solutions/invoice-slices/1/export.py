"""The invoices export Finance reads: CSV."""

import csv
from decimal import ROUND_HALF_UP, Decimal

COLUMNS = ["id", "customer", "issued", "amount_cents"]


def cents(amount):
    """amount, a decimal string, in whole cents rounded half up."""
    return int((Decimal(amount) * 100).quantize(Decimal("1"), rounding=ROUND_HALF_UP))


def export_csv(invoices, path):
    """Writes invoices to path as CSV, sorted by issue date, then id."""
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(COLUMNS)
        for inv in sorted(invoices, key=lambda i: (i.issued, i.id)):
            w.writerow([inv.id, inv.customer, inv.issued.isoformat(), cents(inv.amount)])
