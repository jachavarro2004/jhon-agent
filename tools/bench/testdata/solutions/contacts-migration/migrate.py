"""Move the contacts file from schema 1 to schema 2 (docs/DATA.md)."""

import json
import os
import shutil
import tempfile

TARGET = 2


def split_name(name):
    """The first word, and the rest ("" when there is none)."""
    first, _, last = name.strip().partition(" ")
    return first, last.strip()


def migrate(path):
    """Split each contact's name, keeping every other field; a no-op at schema 2."""
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    if data.get("schema") == TARGET:
        return
    if data.get("schema") != 1:
        raise ValueError("cannot migrate schema %r" % data.get("schema"))
    for contact in data["contacts"]:
        contact["first_name"], contact["last_name"] = split_name(contact.pop("name"))
    data["schema"] = TARGET
    shutil.copyfile(path, path + ".bak")
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(os.path.abspath(path)), suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=2, ensure_ascii=False)
            f.write("\n")
        os.replace(tmp, path)
    except BaseException:
        os.unlink(tmp)
        raise
