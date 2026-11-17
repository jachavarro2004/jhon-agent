"""Every message at the end: message 1's export, message 2's amount, message 3's --since."""

import unittest

from test_m1 import ExportTest  # noqa: F401
from test_m2 import AmountTest  # noqa: F401
from test_m3 import SinceTest  # noqa: F401

if __name__ == "__main__":
    unittest.main()
