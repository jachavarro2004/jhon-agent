"""Acme billing's command line: python3 cli.py <command> <args>."""

import sys

import export
import invoices

USAGE = "usage: python3 cli.py list <invoices.json>\n       python3 cli.py export <invoices.json> <out.csv>"


def main(argv):
    if len(argv) == 2 and argv[0] == "list":
        for inv in invoices.load(argv[1]):
            print(inv.id, inv.customer, inv.issued.isoformat(), inv.amount, sep="\t")
        return 0
    if len(argv) == 3 and argv[0] == "export":
        export.export_csv(invoices.load(argv[1]), argv[2])
        return 0
    print(USAGE, file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
