# Billing rules

These rules hold for every calculation in this project, old and new.

1. **Cents.** Money is an int number of cents. A percentage of money is
   rounded half up to the cent with `money.percent_of`. Never use `round()`,
   which rounds half to even, or `int()`, which truncates.
2. **The balance.** A fee or discount given as a percentage applies to the
   invoice's balance (`billing.balance`: the amount minus its credits), never
   to the gross amount.
3. **Days late.** Lateness counts whole calendar days after the due date:
   paying on the due date is 0 days late. "More than N days late" means
   strictly more than N.
