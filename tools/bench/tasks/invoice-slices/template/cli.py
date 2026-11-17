"""Acme billing's command line: python3 cli.py <command> <args>."""

import sys

import invoices


def main(argv):
    if len(argv) == 2 and argv[0] == "list":
        for inv in invoices.load(argv[1]):
            print(inv.id, inv.customer, inv.issued.isoformat(), inv.amount, sep="\t")
        return 0
    print("usage: python3 cli.py list <invoices.json>", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
