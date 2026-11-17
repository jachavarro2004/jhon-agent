"""Money in integer cents."""
from decimal import ROUND_HALF_UP, Decimal


def percent_of(cents, percent):
    """percent% of cents, rounded half up to a whole cent."""
    return int((Decimal(cents) * Decimal(str(percent)) / 100).quantize(Decimal(1), rounding=ROUND_HALF_UP))
