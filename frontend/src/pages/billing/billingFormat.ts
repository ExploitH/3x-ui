const MINOR_UNIT_CURRENCIES: Record<string, string> = {
  CNY: '¥',
  USD: '$',
};

export function formatBillingMinor(currency: string, amountMinor: number): string {
  const normalized = (currency || '').trim().toUpperCase();
  const amount = Number.isFinite(amountMinor) ? amountMinor : 0;
  const symbol = MINOR_UNIT_CURRENCIES[normalized];
  if (symbol) return `${symbol}${(amount / 100).toFixed(2)}`;
  return normalized ? `${amount} minor ${normalized}` : `${amount} minor`;
}
