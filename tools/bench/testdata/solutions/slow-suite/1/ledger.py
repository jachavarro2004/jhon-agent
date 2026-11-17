"""Acme's ledger: balances and monthly totals of postings, in cents."""

# The kinds that take money from an account.
OUT = ("debit", "refund")


def signed(entry):
    """The entry's amount as it moves the account: + for money in, - for money out."""
    if entry["kind"] in OUT:
        return -entry["amount_cents"]
    return entry["amount_cents"]


def balance(entries, account):
    """The account's balance in cents."""
    return sum(signed(e) for e in entries if e["account"] == account)


def monthly_totals(entries):
    """Net cents per month of every account: {"YYYY-MM": cents}, months in order."""
    totals = {}
    for e in entries:
        month = e["date"][:7]
        totals[month] = totals.get(month, 0) + signed(e)
    return dict(sorted(totals.items()))
