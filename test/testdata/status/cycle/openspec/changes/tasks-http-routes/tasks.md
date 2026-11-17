# Tasks: Add Security Response Headers

## Review Workload Forecast

| Field | Value |
|-------|-------|
| Estimated changed lines | ~120–160 (app/main.py ~30, tests/test_security_headers.py ~90–120) |
| 400-line budget risk | Low |
| Chained PRs recommended | No |
| Suggested split | Single PR |
| Delivery strategy | none cached (default ask-on-risk) |
| Chain strategy | pending |

Decision needed before apply: No
Chained PRs recommended: No
Chain strategy: pending
400-line budget risk: Low

### Suggested Work Units

| Unit | Goal | Likely PR | Focused test command | Runtime harness | Rollback boundary |
|------|------|-----------|----------------------|-----------------|-------------------|
| 1 | Security headers middleware + full test coverage | PR 1 | `uv run --group dev pytest tests/test_security_headers.py` | `uv run --group dev pytest` (full suite, confirms `tests/test_routes.py` unchanged) | Revert the single commit; no routes/deps/config added |

## Phase 1: RED — Failing Tests

- [ ] 1.1 Create `tests/test_security_headers.py` with `client = TestClient(app)` and a `pytest.mark.parametrize` test asserting the four uniform headers (`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `X-Frame-Options: DENY`, `Permissions-Policy: camera=(), geolocation=(), microphone=(), payment=(), usb=()`) on `/health`, `/version`, `/ready`, `/does-not-exist` (404), `POST /health` (405), `/docs`, `/redoc`, `/openapi.json`. Witness: RED.
- [ ] 1.2 Add a parametrized test asserting the exact `Content-Security-Policy` value `default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'` on `/health`, `/version`, `/ready`, `/does-not-exist` (404), `POST /health` (405). Witness: RED.
- [ ] 1.3 Run `uv run --group dev pytest tests/test_security_headers.py`; observe both new tests fail because no header is set. Record the RED witness entries.

## Phase 2: GREEN — Implementation

- [ ] 2.1 In `app/main.py`, add `SECURITY_HEADERS`, `CONTENT_SECURITY_POLICY_HEADER`, `CONTENT_SECURITY_POLICY`, and `DOCS_PATHS` constants after `app = FastAPI()`, per design's Interfaces/Contracts.
- [ ] 2.2 In `app/main.py`, add `security_headers_middleware` (`@app.middleware("http")`) that sets the four `SECURITY_HEADERS` entries on every response; register it after `response_time_middleware`.
- [ ] 2.3 In `app/main.py`, add `content_security_policy_middleware` that sets `CONTENT_SECURITY_POLICY_HEADER` unless `request.url.path in DOCS_PATHS`; register it after `security_headers_middleware`.
- [ ] 2.4 Run `uv run --group dev pytest tests/test_security_headers.py`; observe both Phase 1 tests pass (GREEN).

## Phase 3: Mutation-Witnessed CSP Exclusion

- [ ] 3.1 Add a parametrized test asserting no `Content-Security-Policy` header on `/docs`, `/redoc`, `/openapi.json`.
- [ ] 3.2 Run `uv run --group dev pytest tests/test_security_headers.py`; observe the new test passes immediately (guard already present).
- [ ] 3.3 Mutation witness: temporarily remove the `DOCS_PATHS` guard in `content_security_policy_middleware` (make CSP unconditional); rerun 3.1's test and observe it fail; restore the guard; rerun and observe green. Record the mutation witness entry.

## Phase 4: Regression Verification

- [ ] 4.1 Run `uv run --group dev pytest` (full suite); confirm `tests/test_routes.py` is byte-unchanged and passing, alongside every `tests/test_security_headers.py` test.
- [ ] 4.2 Populate the TDD Cycle Evidence table and Witness report entries: rows 1.1/1.2 → `RED`, row 3.1 → `mutation`, rows 2.1–2.3 → `N/A (production code)`, per `contracts/tdd.md`.

