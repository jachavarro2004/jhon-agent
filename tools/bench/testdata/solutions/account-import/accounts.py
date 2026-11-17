"""Load accounts from CSV text."""

import csv
import io
import re

AGE = re.compile(r"^[0-9]+$")


def load_accounts(text):
    """Return the accounts in CSV text with the header email,name,age.

    Each account is a dict with the email trimmed and lowercased, the name
    trimmed and the age as an int. The first invalid row raises ValueError
    naming its line.
    """
    reader = csv.DictReader(io.StringIO(text))
    accounts = []
    seen = set()
    for row in reader:
        line = reader.line_num
        email = row["email"].strip().lower()
        local, at, domain = email.partition("@")
        if not at or not local or not domain or "@" in domain:
            raise ValueError("line %d: invalid email %r" % (line, row["email"]))
        if email in seen:
            raise ValueError("line %d: repeated email %r" % (line, email))
        age = row["age"].strip()
        if not AGE.match(age) or int(age) > 150:
            raise ValueError("line %d: invalid age %r" % (line, row["age"]))
        seen.add(email)
        accounts.append({"email": email, "name": row["name"].strip(), "age": int(age)})
    return accounts
