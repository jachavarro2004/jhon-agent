"""Money helpers. Money is an int number of cents."""

from decimal import ROUND_HALF_UP, Decimal


def percent_of(cents, percent):
    """percent % of cents, rounded half up to the cent (docs/RULES.md)."""
    exact = Decimal(cents) * Decimal(str(percent)) / 100
    return int(exact.quantize(Decimal(1), rounding=ROUND_HALF_UP))


def format_cents(cents):
    """cents as a price, such as 12.05."""
    return "%d.%02d" % divmod(cents, 100)
