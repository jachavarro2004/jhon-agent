"""The full suite: every scenario in suite/scenarios.json against the ledger.

Each scenario replays a day of postings at the pace the bank's feed sends
them, so the whole suite takes about 90 seconds.
"""

import json
import os
import sys
import time

import ledger

HERE = os.path.dirname(os.path.abspath(__file__))
SCENARIO_SECONDS = 3.0


def main():
    with open(os.path.join(HERE, "suite", "scenarios.json"), encoding="utf-8") as f:
        scenarios = json.load(f)
    failed = 0
    for s in scenarios:
        time.sleep(SCENARIO_SECONDS)
        got_balance = ledger.balance(s["entries"], s["account"])
        got_months = ledger.monthly_totals(s["entries"])
        if got_balance == s["balance"] and got_months == s["months"]:
            print(f"ok    {s['name']}", flush=True)
            continue
        failed += 1
        print(f"FAIL  {s['name']}: balance of {s['account']} {got_balance}, want {s['balance']}; "
              f"months {got_months}, want {s['months']}", flush=True)
    print(f"{len(scenarios)} scenarios, {failed} failed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
