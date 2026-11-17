"""Invoices.

An invoice is a dict: "amount" (int cents), "credits" (int cents already
credited against it) and "due" (a datetime.date).
"""

from money import percent_of

LATE_AFTER_DAYS = 30
LATE_FEE_PERCENT = 5


def balance(invoice):
    """What is left to pay: the amount minus the credits."""
    return invoice["amount"] - invoice["credits"]


def days_late(invoice, paid_on):
    """Whole days paid_on is after the due date, 0 when on time."""
    return max(0, (paid_on - invoice["due"]).days)


def late_fee(invoice, paid_on):
    """The late fee in cents: 5% of the balance when paid more than 30 days
    late, else 0 (docs/RULES.md)."""
    if days_late(invoice, paid_on) > LATE_AFTER_DAYS:
        return percent_of(balance(invoice), LATE_FEE_PERCENT)
    return 0
