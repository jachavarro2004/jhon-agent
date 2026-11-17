"""Exports products to the storefront's JSON Lines by docs/EXPORT.md."""

import json


def export_products(products, path):
    """Writes products to path, sorted by sku, free samples left out."""
    with open(path, "w", encoding="utf-8", newline="\n") as f:
        for p in sorted(products, key=lambda p: p.sku):
            if p.price_cents == 0:
                continue
            price = f"{p.price_cents // 100}.{p.price_cents % 100:02d}"
            f.write(json.dumps({"sku": p.sku, "name": p.name, "price": price, "tags": list(p.tags)}, ensure_ascii=False) + "\n")
