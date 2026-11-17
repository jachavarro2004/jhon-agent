"""Search a list of names."""

import unicodedata

from textutil import normalize


def fold(text):
    """normalize(text) with its accents dropped, for matching only."""
    text = unicodedata.normalize("NFKD", normalize(text))
    return "".join(c for c in text if not unicodedata.combining(c))


def search(items, query):
    """Return the items that contain the query, ignoring case, spacing and accents."""
    q = fold(query)
    return [item for item in items if q in fold(item)]
