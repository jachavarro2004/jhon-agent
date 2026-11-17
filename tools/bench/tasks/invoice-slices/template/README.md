# Acme billing

Invoices come from the billing system's JSON export (`data/invoices.json`):
a list of objects with `id`, `customer`, `issued` (YYYY-MM-DD) and `amount`,
a decimal string with up to three decimals.

    python3 cli.py list data/invoices.json

Tests: `python3 -m unittest`
