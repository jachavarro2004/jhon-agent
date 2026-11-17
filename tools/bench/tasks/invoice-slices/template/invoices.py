"""Acme's invoices, as the billing system exports them."""

import json
from dataclasses import dataclass
from datetime import date


@dataclass(frozen=True)
class Invoice:
    id: str
    customer: str
    issued: date
    amount: str  # a decimal string, up to three decimals: "10.005", "7"


def load(path):
    """The invoices in the JSON export at path, in the file's order."""
    with open(path, encoding="utf-8") as f:
        rows = json.load(f)
    return [Invoice(r["id"], r["customer"], date.fromisoformat(r["issued"]), r["amount"]) for r in rows]
