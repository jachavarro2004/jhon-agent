"""Load accounts from CSV text."""

import csv
import io


def load_accounts(text):
    """Return the accounts in CSV text with the header email,name,age.

    Each account is a dict with the email trimmed and lowercased, the name
    trimmed and the age as an int.
    """
    reader = csv.DictReader(io.StringIO(text))
    accounts = []
    for row in reader:
        accounts.append({
            "email": row["email"].strip().lower(),
            "name": row["name"].strip(),
            "age": int(row["age"]),
        })
    return accounts
