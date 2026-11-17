"""Text helpers shared by the whole project."""


def normalize(text):
    """Trim the text, collapse inner whitespace and casefold it."""
    return " ".join(text.split()).casefold()
