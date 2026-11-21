# Ledger rules

## Amounts

An amount is kept as a whole number of cents, always positive. Written by
hand it is a number with at most two decimals (`12`, `12.5`, `12.50`); the
command line prints it with exactly two (`12.50`). Anything else (a sign,
three decimals, a comma) is not an amount.

## Transactions

Every transaction has an id (`t1`, `t2` … given by the ledger), a date
(`YYYY-MM-DD`, a real calendar day), a kind, a category and an amount. Its
month is the date's `YYYY-MM`.

The kind says which way the money went:

| Kind | Balance | Meaning |
|---|---|---|
| `income` | adds the amount | money earned |
| `expense` | subtracts the amount | money spent in the category |
| `refund` | adds the amount | money given back for an expense of the category; it undoes spending, it is not income |
| `transfer` | unchanged | money moved between the owner's own accounts (the category names the account); it is neither spending nor income |

## The ledger file

One JSON object. `transactions` is the list of transactions. A key the
ledger does not know is kept as it is when the ledger writes the file. The
file is written whole to a temporary file beside it and renamed over it, so
a failed write never leaves half a file.
