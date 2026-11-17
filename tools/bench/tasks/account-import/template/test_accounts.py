import unittest

from accounts import load_accounts


class LoadAccountsTest(unittest.TestCase):
    def test_loads_trimmed_rows(self):
        text = "email,name,age\n Ana@Example.com , Ana ,34\nbo@example.com,Bo,7\n"
        self.assertEqual(load_accounts(text), [
            {"email": "ana@example.com", "name": "Ana", "age": 34},
            {"email": "bo@example.com", "name": "Bo", "age": 7},
        ])

    def test_header_only_is_empty(self):
        self.assertEqual(load_accounts("email,name,age\n"), [])


if __name__ == "__main__":
    unittest.main()
