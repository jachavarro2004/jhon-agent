"""Shipping prices, in cents."""

ZONES = {"local": 500, "national": 1200}


def format_price(cents):
    """Return cents as a price string, such as 12.50."""
    return "%d.%02d" % divmod(cents, 100)


def shipping_cost(weight_kg, zone, express):
    """Return the shipping price in cents."""
    raise NotImplementedError
