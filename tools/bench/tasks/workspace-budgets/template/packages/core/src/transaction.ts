// Transactions and their kinds (docs/LEDGER.md, "Transactions").

export type Kind = "income" | "expense" | "refund" | "transfer";

export const KINDS: readonly Kind[] = ["income", "expense", "refund", "transfer"];

export interface Transaction {
  id: string;
  date: string; // YYYY-MM-DD
  kind: Kind;
  category: string;
  amountCents: number;
}

const DATE = /^(\d{4})-(\d{2})-(\d{2})$/;

/** Whether text is a real calendar day written YYYY-MM-DD. */
export function isDate(text: string): boolean {
  const m = DATE.exec(text);
  if (m === null) {
    return false;
  }
  const [year, month, day] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const d = new Date(Date.UTC(year, month - 1, day));
  return d.getUTCFullYear() === year && d.getUTCMonth() === month - 1 && d.getUTCDate() === day;
}

/** The month of a date: its YYYY-MM. */
export function monthOf(date: string): string {
  return date.slice(0, 7);
}

export function isKind(text: string): text is Kind {
  return (KINDS as readonly string[]).includes(text);
}

/** Throws a RangeError naming the first field of tx that breaks the rules. */
export function validateTransaction(tx: Omit<Transaction, "id">): void {
  if (!isDate(tx.date)) {
    throw new RangeError(`not a date: ${JSON.stringify(tx.date)}`);
  }
  if (!isKind(tx.kind)) {
    throw new RangeError(`not a kind: ${JSON.stringify(tx.kind)} (kinds: ${KINDS.join(", ")})`);
  }
  if (typeof tx.category !== "string" || tx.category.trim() === "") {
    throw new RangeError("a transaction needs a category");
  }
  if (!Number.isSafeInteger(tx.amountCents) || tx.amountCents <= 0) {
    throw new RangeError(`not an amount in cents: ${tx.amountCents}`);
  }
}

/** What tx does to the balance, in cents. */
export function balanceEffect(tx: Transaction): number {
  switch (tx.kind) {
    case "income":
    case "refund":
      return tx.amountCents;
    case "expense":
      return -tx.amountCents;
    case "transfer":
      return 0;
  }
}

/** Transactions by date, then by id. */
export function byDate(a: Transaction, b: Transaction): number {
  if (a.date !== b.date) {
    return a.date < b.date ? -1 : 1;
  }
  return Number(a.id.slice(1)) - Number(b.id.slice(1));
}
