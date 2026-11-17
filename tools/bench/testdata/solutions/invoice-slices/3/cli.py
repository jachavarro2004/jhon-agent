"""Acme billing's command line: python3 cli.py <command> <args>."""

import sys
from datetime import date

import export
import invoices

USAGE = "usage: python3 cli.py list <invoices.json>\n       python3 cli.py export <invoices.json> <out.csv> [--since YYYY-MM-DD]"


def since_date(text):
    """text as a date when it is a valid YYYY-MM-DD, else None."""
    if len(text) != 10:
        return None
    try:
        return date.fromisoformat(text)
    except ValueError:
        return None


def main(argv):
    if len(argv) == 2 and argv[0] == "list":
        for inv in invoices.load(argv[1]):
            print(inv.id, inv.customer, inv.issued.isoformat(), inv.amount, sep="\t")
        return 0
    if argv[:1] == ["export"] and len(argv) in (3, 5) and (len(argv) == 3 or argv[3] == "--since"):
        rows = invoices.load(argv[1])
        if len(argv) == 5:
            since = since_date(argv[4])
            if since is None:
                print(f"cli.py: --since {argv[4]!r} is not a valid YYYY-MM-DD date", file=sys.stderr)
                return 2
            rows = [inv for inv in rows if inv.issued >= since]
        export.export_csv(rows, argv[2])
        return 0
    print(USAGE, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
