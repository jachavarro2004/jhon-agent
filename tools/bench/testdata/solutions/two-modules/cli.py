"""Acme catalog's command line: python3 cli.py <command> <args>."""

import sys

import exporter
import importer


def main(argv):
    if len(argv) == 3 and argv[0] == "convert":
        products, rejected = importer.import_products(argv[1])
        exporter.export_products(products, argv[2])
        print(f"{len(products)} products, {rejected} rejected")
        return 0
    print("usage: python3 cli.py convert <supplier.csv> <out.jsonl>", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
