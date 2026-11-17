"""Move the contacts file from schema 1 to schema 2."""

import json


def migrate(path):
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    contacts = []
    for c in data["contacts"]:
        first, _, last = c["name"].partition(" ")
        contacts.append({"id": c["id"], "email": c["email"], "first_name": first, "last_name": last})
    with open(path, "w", encoding="utf-8") as f:
        json.dump({"schema": 2, "contacts": contacts}, f, indent=2)
