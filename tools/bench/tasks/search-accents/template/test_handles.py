import unittest

from handles import handle_key, register


class HandlesTest(unittest.TestCase):
    def test_case_and_spacing_are_the_same_handle(self):
        taken = set()
        register("Ana  Ruiz", taken)
        with self.assertRaises(ValueError):
            register("ana ruiz", taken)

    def test_accents_make_a_different_handle(self):
        taken = set()
        register("José", taken)
        register("Jose", taken)
        self.assertEqual(len(taken), 2)
        self.assertNotEqual(handle_key("José"), handle_key("Jose"))


if __name__ == "__main__":
    unittest.main()
