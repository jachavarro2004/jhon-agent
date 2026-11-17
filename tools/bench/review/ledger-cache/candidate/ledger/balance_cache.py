"""A process-wide cache of account balances, filled by worker threads."""
import subprocess
import threading

from ledger.store import compute_balance

_cache = {}
_hits = 0
_lock = threading.Lock()


def balance(account, compute=compute_balance):
    global _hits
    if account in _cache:
        _hits += 1
        return _cache[account]
    value = compute(account)
    _cache[account] = value
    return value


def refresh_all(accounts, compute=compute_balance):
    """Recomputes every account at once, one thread per account."""
    _cache.clear()
    threads = [threading.Thread(target=balance, args=(a, compute)) for a in accounts]
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    return dict(_cache)


def export_snapshot(target):
    """Copies the ledger next to the cache's numbers for the nightly report."""
    subprocess.run(["cp", "ledger.db", target])
    return target
