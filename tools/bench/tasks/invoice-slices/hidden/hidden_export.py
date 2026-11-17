"""What the hidden checks share: a fixture export and a run of the export command."""

import csv
import json
import os
import subprocess
import sys
import tempfile

HERE = os.path.dirname(os.path.abspath(__file__))

ROWS = [
    {"id": "INV-001", "customer": "Umbrella", "issued": "2026-02-28", "amount": "7"},
    {"id": "INV-002", "customer": "Globex", "issued": "2026-01-15", "amount": "2.675"},
    {"id": "INV-003", "customer": 'Initech "East"', "issued": "2026-03-02", "amount": "0.125"},
    {"id": "INV-004", "customer": "Acme, Inc.", "issued": "2026-03-02", "amount": "10.005"},
]

# By issue date, then id.
ORDER = ["INV-002", "INV-001", "INV-003", "INV-004"]


def export(*extra):
    """Runs cli.py export on ROWS: (exit code, stderr, CSV rows or None)."""
    with tempfile.TemporaryDirectory() as d:
        src, out = os.path.join(d, "invoices.json"), os.path.join(d, "out.csv")
        with open(src, "w", encoding="utf-8") as f:
            json.dump(ROWS, f)
        p = subprocess.run([sys.executable, "-B", os.path.join(HERE, "cli.py"), "export", src, out, *extra],
                           capture_output=True, text=True, cwd=HERE, timeout=60)
        rows = None
        if os.path.exists(out):
            with open(out, newline="", encoding="utf-8") as f:
                rows = list(csv.reader(f))
        return p.returncode, p.stderr, rows
