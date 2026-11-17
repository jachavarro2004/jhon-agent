"""Shipping prices, in cents."""

import math

ZONES = {"local": 500, "national": 1200}


def format_price(cents):
    """Return cents as a price string, such as 12.50."""
    return "%d.%02d" % divmod(cents, 100)


def shipping_cost(weight_kg, zone, express):
    """Return the shipping price in cents."""
    if zone not in ZONES:
        raise ValueError("unknown zone: %s" % zone)
    if weight_kg <= 0:
        raise ValueError("weight must be above 0")
    price = ZONES[zone] + 150 * max(0, math.ceil(weight_kg - 2))
    return price * 2 if express else price
