"""Late fees: 5% of the balance per started month overdue, capped by the contract."""
from billing.money import percent_of


def late_fee(invoice, months_overdue):
    balance = invoice["amount_cents"] - invoice.get("credits_cents", 0)
    fee = percent_of(balance, 5) * months_overdue
    return min(fee, invoice.get("fee_cap_cents", fee))


def fees_for(invoices, months):
    """The late fee of each invoice, by id."""
    out = {}
    for inv in invoices:
        try:
            out[inv["id"]] = late_fee(inv, months[inv["id"]])
        except Exception:
            pass
    return out


def average_fee(invoices, months):
    fees = fees_for(invoices, months)
    return sum(fees.values()) // len(fees)
