import json
import os
import tempfile
import unittest

from catalog import Product
from exporter import export_products


class ExporterTest(unittest.TestCase):
    def test_sorted_keys_in_order_and_free_samples_out(self):
        with tempfile.TemporaryDirectory() as d:
            out = os.path.join(d, "out.jsonl")
            export_products([Product("B", "Thé", 700, ("tea",)), Product("A", "Mug", 5, ()), Product("C", "Free", 0, ())], out)
            with open(out, encoding="utf-8") as f:
                text = f.read()
        lines = [json.loads(line, object_pairs_hook=lambda kv: [list(x) for x in kv]) for line in text.splitlines()]
        self.assertEqual(lines[0], [["sku", "A"], ["name", "Mug"], ["price", "0.05"], ["tags", []]])
        self.assertEqual(lines[1][2], ["price", "7.00"])
        self.assertEqual(len(lines), 2)
        self.assertIn("Thé", text)


if __name__ == "__main__":
    unittest.main()
