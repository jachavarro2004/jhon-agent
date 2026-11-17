"""Invoices.

An invoice is a dict: "amount" (int cents), "credits" (int cents already
credited against it) and "due" (a datetime.date).
"""


def balance(invoice):
    """What is left to pay: the amount minus the credits."""
    return invoice["amount"] - invoice["credits"]


def days_late(invoice, paid_on):
    """Whole days paid_on is after the due date, 0 when on time."""
    return max(0, (paid_on - invoice["due"]).days)
