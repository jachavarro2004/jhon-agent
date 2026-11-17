"""User handles: each one is unique by its key."""

from textutil import normalize


def handle_key(handle):
    """The key a handle is unique by.

    Case and spacing do not make a new handle, but accents do: "José" and
    "Jose" are two different people and must both be able to register.
    """
    return normalize(handle)


def register(handle, taken):
    """Add handle's key to the set taken; ValueError if it is already there."""
    key = handle_key(handle)
    if key in taken:
        raise ValueError("handle taken: " + handle)
    taken.add(key)
    return key
