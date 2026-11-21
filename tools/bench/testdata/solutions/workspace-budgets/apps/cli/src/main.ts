#!/usr/bin/env node
// The ledger command line (README.md).

import { formatCents, isKind, isMonth, parseAmount } from "../../../packages/core/src/index.ts";
import { Ledger } from "../../../packages/api/src/index.ts";

export interface IO {
  out: (line: string) => void;
  err: (line: string) => void;
}

const USAGE = `usage: ledger <ledger.json> add <YYYY-MM-DD> <kind> <category> <amount>
       ledger <ledger.json> list [YYYY-MM]
       ledger <ledger.json> balance
       ledger <ledger.json> budget set <category> <amount>
       ledger <ledger.json> budget report <YYYY-MM>`;

const MONTH = /^\d{4}-(0[1-9]|1[0-2])$/;

class UsageError extends Error {}

/** Runs the command line on argv (without node and the script) and returns its exit status. */
export function main(argv: string[], io: IO): number {
  try {
    return run(argv, io);
  } catch (e) {
    if (e instanceof UsageError) {
      io.err(`${e.message}\n${USAGE}`);
      return 2;
    }
    if (e instanceof RangeError) {
      io.err(`error: ${e.message}`);
      return 2;
    }
    throw e;
  }
}

function run(argv: string[], io: IO): number {
  const [file, command, ...args] = argv;
  if (file === undefined || command === undefined) {
    throw new UsageError("missing the ledger file or the command");
  }
  switch (command) {
    case "add": {
      if (args.length !== 4) {
        throw new UsageError("add takes a date, a kind, a category and an amount");
      }
      const [date, kind, category, amount] = args;
      if (!isKind(kind)) {
        throw new RangeError(`not a kind: ${JSON.stringify(kind)}`);
      }
      const cents = parseAmount(amount);
      const tx = Ledger.open(file).add({ date, kind, category, amountCents: cents });
      io.out(`added ${tx.id}`);
      return 0;
    }
    case "list": {
      if (args.length > 1 || (args.length === 1 && !MONTH.test(args[0]))) {
        throw new RangeError(`not a month (YYYY-MM): ${JSON.stringify(args.join(" "))}`);
      }
      for (const t of Ledger.open(file).transactions(args[0])) {
        io.out(`${t.id} ${t.date} ${t.kind} ${t.category} ${formatCents(t.amountCents)}`);
      }
      return 0;
    }
    case "balance": {
      io.out(`balance ${formatCents(Ledger.open(file).balanceCents())}`);
      return 0;
    }
    case "budget":
      return budget(file, args, io);
  }
  throw new UsageError(`unknown command: ${command}`);
}

function budget(file: string, args: string[], io: IO): number {
  const [sub, ...rest] = args;
  if (sub === "set" && rest.length === 2) {
    const [category, amount] = rest;
    const cents = parseAmount(amount);
    Ledger.open(file).setBudget(category, cents);
    io.out(`budget ${category} ${formatCents(cents)}`);
    return 0;
  }
  if (sub === "report" && rest.length === 1) {
    if (!isMonth(rest[0])) {
      throw new RangeError(`not a month (YYYY-MM): ${JSON.stringify(rest[0])}`);
    }
    for (const row of Ledger.open(file).budgetReport(rest[0])) {
      const over = row.over ? " OVER" : "";
      io.out(`${row.category} ${formatCents(row.spentCents)} / ${formatCents(row.limitCents)} (${row.percentUsed}%)${over}`);
    }
    return 0;
  }
  throw new UsageError("budget takes set <category> <amount> or report <YYYY-MM>");
}

if (import.meta.main) {
  process.exitCode = main(process.argv.slice(2), {
    out: (line) => process.stdout.write(line + "\n"),
    err: (line) => process.stderr.write(line + "\n"),
  });
}
