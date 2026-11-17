"""Split a list into pages."""


def page_count(total, size):
    """Return how many pages of the given size hold total items."""
    if size <= 0:
        raise ValueError("size must be positive")
    return (total + size - 1) // size


def paginate(items, page, size):
    """Return the items on a 1-based page of the given size."""
    if size <= 0:
        raise ValueError("size must be positive")
    if page < 1:
        raise ValueError("page must be 1 or more")
    start = (page - 1) * size
    return items[start:start + size]
