"""The set-up run's hidden check: LDD is set up and the code is unchanged."""
import os
import unittest

from greet import greet


class SetupAcceptance(unittest.TestCase):
    def test_ldd_is_set_up(self):
        self.assertTrue(os.path.isfile(os.path.join("ldd", "config.yaml")) or os.path.isfile(os.path.join("openspec", "config.yaml")),
                        "no ldd/config.yaml (or openspec/config.yaml)")

    def test_code_unchanged(self):
        self.assertEqual(greet("Ana"), "Hello, Ana!")


if __name__ == "__main__":
    unittest.main()
