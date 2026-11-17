"""Read the contacts file."""

import json

SCHEMA = 1


def load(path):
    """Return the contacts in the file at path.

    Each contact is a dict with id, name and email (and whatever else the
    file carries). A file at another schema raises ValueError.
    """
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    if data.get("schema") != SCHEMA:
        raise ValueError("unsupported schema: %r" % data.get("schema"))
    return data["contacts"]


def display_name(contact):
    """The name to show for a contact."""
    return contact["name"]
