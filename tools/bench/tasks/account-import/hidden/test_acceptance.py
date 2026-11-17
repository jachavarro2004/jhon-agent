import unittest

from accounts import load_accounts

HEADER = "email,name,age\n"
GOOD = "ana@example.com,Ana,34\n"


class AcceptanceTest(unittest.TestCase):
    def assertRejected(self, row, line):
        with self.assertRaises(ValueError) as caught:
            load_accounts(HEADER + GOOD + row)
        self.assertTrue(str(caught.exception).startswith("line %d:" % line), str(caught.exception))

    def test_valid_input_loads_as_before(self):
        text = HEADER + " Ana@Example.com , Ana ,34\nbo@example.com,Bo,0\ncy@example.com,Cy,150\n"
        self.assertEqual(load_accounts(text), [
            {"email": "ana@example.com", "name": "Ana", "age": 34},
            {"email": "bo@example.com", "name": "Bo", "age": 0},
            {"email": "cy@example.com", "name": "Cy", "age": 150},
        ])

    def test_rejects_a_bad_email(self):
        self.assertRejected("bo.example.com,Bo,7\n", 3)
        self.assertRejected("bo@@example.com,Bo,7\n", 3)
        self.assertRejected("@example.com,Bo,7\n", 3)
        self.assertRejected("bo@,Bo,7\n", 3)

    def test_rejects_a_bad_age(self):
        self.assertRejected("bo@example.com,Bo,-1\n", 3)
        self.assertRejected("bo@example.com,Bo,151\n", 3)
        self.assertRejected("bo@example.com,Bo,7.5\n", 3)
        self.assertRejected("bo@example.com,Bo,seven\n", 3)

    def test_rejects_a_repeated_email_ignoring_case(self):
        self.assertRejected("ANA@example.com,Ana B,40\n", 3)

    def test_names_the_first_invalid_line(self):
        with self.assertRaises(ValueError) as caught:
            load_accounts(HEADER + GOOD + "bo@example.com,Bo,7\nbad,Cy,1\nworse,Di,-4\n")
        self.assertTrue(str(caught.exception).startswith("line 4:"), str(caught.exception))


if __name__ == "__main__":
    unittest.main()
