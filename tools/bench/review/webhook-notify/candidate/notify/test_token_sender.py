import unittest

from notify.token_sender import build_request


class BuildRequest(unittest.TestCase):
    def test_bearer(self):
        req = build_request({"id": 7}, "t0k")
        self.assertEqual(req.get_header("Authorization"), "Bearer t0k")
        self.assertEqual(req.get_method(), "POST")


if __name__ == "__main__":
    unittest.main()
