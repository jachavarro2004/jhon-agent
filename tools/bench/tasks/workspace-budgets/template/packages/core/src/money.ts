// Amounts are whole cents, always positive (docs/LEDGER.md, "Amounts").

const AMOUNT = /^(\d+)(?:\.(\d{1,2}))?$/;

/** The cents of an amount written by hand: "12", "12.5" or "12.50". */
export function parseAmount(text: string): number {
  const m = AMOUNT.exec(text.trim());
  if (m === null) {
    throw new RangeError(`not an amount: ${JSON.stringify(text)}`);
  }
  const cents = Number(m[1]) * 100 + Number((m[2] ?? "").padEnd(2, "0"));
  if (!Number.isSafeInteger(cents)) {
    throw new RangeError(`amount too large: ${text}`);
  }
  return cents;
}

/** cents with exactly two decimals: 1250 is "12.50", -5 is "-0.05". */
export function formatCents(cents: number): string {
  const sign = cents < 0 ? "-" : "";
  const abs = Math.abs(cents);
  return `${sign}${Math.floor(abs / 100)}.${String(abs % 100).padStart(2, "0")}`;
}
