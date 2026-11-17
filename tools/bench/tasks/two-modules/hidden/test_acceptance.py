"""The importer and the exporter by docs/IMPORT.md and docs/EXPORT.md, and the convert command."""

import json
import os
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))

CSV = (
    "﻿sku;name;price;tags\n"
    " ab-1 ;Mug  large;12,50;Kitchen|kitchen| gift\n"
    "AB-2;Café \t tin;1.234,56;\n"
    ";No sku;3,00;x\n"
    "ab-3;Broken;3,5x;\n"
    "ab-1;Mug  XL;13,00;kitchen||Gift\n"
    "AB-4;Sample;0;free\n"
    "AB-5;Thousand;1.000;\n"
    "AB-6;Too precise;1,234;\n"
    "AB-7;Negative;-5,00;\n"
)


def write(d, text):
    path = os.path.join(d, "supplier.csv")
    with open(path, "w", encoding="utf-8", newline="") as f:
        f.write(text)
    return path


class ImportTest(unittest.TestCase):
    def test_the_rules_of_docs_import(self):
        from catalog import Product
        from importer import import_products

        with tempfile.TemporaryDirectory() as d:
            products, rejected = import_products(write(d, CSV))
        by_sku = {p.sku: p for p in products}
        self.assertEqual(sorted(by_sku), ["AB-1", "AB-2", "AB-4", "AB-5"])
        self.assertEqual(len(products), 4)
        self.assertEqual(rejected, 4)
        self.assertEqual(by_sku["AB-1"], Product("AB-1", "Mug XL", 1300, ("gift", "kitchen")))
        self.assertEqual(by_sku["AB-2"], Product("AB-2", "Café tin", 123456, ()))
        self.assertEqual(by_sku["AB-4"].price_cents, 0)
        self.assertEqual(by_sku["AB-5"].price_cents, 100000)


class ExportTest(unittest.TestCase):
    def test_the_rules_of_docs_export(self):
        from catalog import Product
        from exporter import export_products

        products = [Product("ZZ-9", "Thé vert", 700, ("tea",)), Product("AB-1", "Mug", 123456, ("gift", "kitchen")),
                    Product("FREE-1", "Sample", 0, ())]
        with tempfile.TemporaryDirectory() as d:
            out = os.path.join(d, "out.jsonl")
            export_products(products, out)
            with open(out, "rb") as f:
                data = f.read()
            export_products([], out)
            with open(out, "rb") as f:
                empty = f.read()
        self.assertTrue(data.endswith(b"\n"))
        self.assertIn("Thé vert".encode("utf-8"), data)
        lines = [json.loads(line, object_pairs_hook=lambda kv: [list(x) for x in kv]) for line in data.decode("utf-8").splitlines()]
        self.assertEqual(lines, [
            [["sku", "AB-1"], ["name", "Mug"], ["price", "1234.56"], ["tags", ["gift", "kitchen"]]],
            [["sku", "ZZ-9"], ["name", "Thé vert"], ["price", "7.00"], ["tags", ["tea"]]],
        ])
        self.assertEqual(empty, b"")


class ConvertTest(unittest.TestCase):
    def test_convert_wires_both(self):
        with tempfile.TemporaryDirectory() as d:
            out = os.path.join(d, "out.jsonl")
            p = subprocess.run([sys.executable, "-B", os.path.join(HERE, "cli.py"), "convert", write(d, CSV), out],
                               capture_output=True, text=True, cwd=HERE, timeout=60)
            self.assertEqual(p.returncode, 0, p.stderr)
            self.assertEqual(p.stdout.strip(), "4 products, 4 rejected")
            with open(out, encoding="utf-8") as f:
                skus = [json.loads(line)["sku"] for line in f]
        self.assertEqual(skus, ["AB-1", "AB-2", "AB-5"])

if __name__ == "__main__":
    unittest.main()
