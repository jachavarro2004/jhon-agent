"""The ledger's postings, read from ledger.db (one JSON posting per line)."""
import json


def postings(path="ledger.db"):
    with open(path, encoding="utf-8") as f:
        return [json.loads(line) for line in f if line.strip()]


def compute_balance(account, path="ledger.db"):
    return sum(p["cents"] for p in postings(path) if p["account"] == account)
