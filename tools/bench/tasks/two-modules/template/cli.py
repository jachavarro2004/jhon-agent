"""Acme catalog's command line: python3 cli.py <command> <args>."""

import sys


def main(argv):
    print("usage: python3 cli.py convert <supplier.csv> <out.jsonl>", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
