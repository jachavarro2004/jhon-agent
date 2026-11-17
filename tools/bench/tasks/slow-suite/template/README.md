# Acme ledger

`ledger.py` reads postings: dicts with `date` (YYYY-MM-DD), `account`,
`kind` (`credit`, `debit` or `refund`) and `amount_cents` (positive). A
credit adds to an account; a debit and a refund take from it.

- Fast unit tests: `python3 -m unittest` (a second).
- Full suite: `python3 run_suite.py`. It replays every scenario in
  `suite/scenarios.json` and takes about 90 seconds.
