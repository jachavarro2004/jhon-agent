# Ledger

A personal-finance ledger, as a pnpm workspace of TypeScript packages:

| Package | What it holds |
|---|---|
| `packages/core` (`@ledger/core`) | money and transactions: amounts in cents, the transaction kinds and their rules |
| `packages/api` (`@ledger/api`) | `Ledger`: the transactions kept in one JSON file |
| `apps/cli` (`@ledger/cli`) | the `ledger` command line over `@ledger/api` |

Node runs the TypeScript sources directly (type stripping, Node 24 or
later): there is no build step and no dependency to install. A package
imports another by its relative path with the `.ts` extension
(`../../core/src/index.ts`). Only erasable TypeScript is allowed (no
`enum`, no parameter properties): see `tsconfig.base.json`.

The rules of amounts and transaction kinds are in `docs/LEDGER.md`.

Tests use `node:test`, next to each package's sources in `test/`:

    node --test          # every package, from the root
    npm test             # one package, from its directory

The command line:

    node apps/cli/src/main.ts <ledger.json> add <YYYY-MM-DD> <kind> <category> <amount>
    node apps/cli/src/main.ts <ledger.json> list [YYYY-MM]
    node apps/cli/src/main.ts <ledger.json> balance
