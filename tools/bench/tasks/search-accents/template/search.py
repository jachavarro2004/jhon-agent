"""Search a list of names."""

from textutil import normalize


def search(items, query):
    """Return the items that contain the query, ignoring case and spacing."""
    q = normalize(query)
    return [item for item in items if q in normalize(item)]
