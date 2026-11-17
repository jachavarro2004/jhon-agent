"""URL slugs for product names."""
import re
import unicodedata

from textutil.normalize import collapse_spaces

_NOT_SLUG = re.compile(r"[^a-z0-9]+")


def slugify(name, max_length=60):
    """Lower-case ASCII words joined by '-', at most max_length characters, never ending in '-'."""
    if max_length < 1:
        raise ValueError("max_length must be at least 1")
    folded = unicodedata.normalize("NFKD", collapse_spaces(name)).encode("ascii", "ignore").decode("ascii")
    slug = _NOT_SLUG.sub("-", folded.lower()).strip("-")
    return slug[:max_length].rstrip("-")
