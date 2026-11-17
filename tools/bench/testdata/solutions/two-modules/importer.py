"""Imports a supplier's CSV by docs/IMPORT.md."""

import csv
import re

from catalog import Product

PRICE = re.compile(r"(\d{1,3}(?:\.\d{3})*|\d+)(?:,(\d{1,2}))?")


def price_cents(text):
    """A European price in cents, or None when it breaks docs/IMPORT.md."""
    m = PRICE.fullmatch(text.strip())
    if not m:
        return None
    whole, decimals = m.group(1).replace(".", ""), (m.group(2) or "").ljust(2, "0")
    return int(whole) * 100 + int(decimals)


def tags_of(text):
    return tuple(sorted({t.strip().lower() for t in text.split("|")} - {""}))


def import_products(path):
    """(products, rejected) of the CSV at path."""
    by_sku, rejected = {}, 0
    with open(path, encoding="utf-8-sig", newline="") as f:
        for row in csv.DictReader(f, delimiter=";"):
            sku = (row.get("sku") or "").strip().upper()
            cents = price_cents(row.get("price") or "")
            if not sku or cents is None:
                rejected += 1
                continue
            name = " ".join((row.get("name") or "").split())
            by_sku[sku] = Product(sku, name, cents, tags_of(row.get("tags") or ""))
    return list(by_sku.values()), rejected
