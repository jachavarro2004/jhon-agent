"""Text helpers shared by the whole project."""

import unicodedata


def normalize(text):
    """Trim the text, collapse inner whitespace, casefold it and drop accents."""
    text = unicodedata.normalize("NFKD", " ".join(text.split()).casefold())
    return "".join(c for c in text if not unicodedata.combining(c))
