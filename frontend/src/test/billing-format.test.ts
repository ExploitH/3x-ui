import { describe, expect, it } from 'vitest';

import { formatBillingMinor } from '@/pages/billing/billingFormat';

describe('billing amount formatting', () => {
  it('formats known CNY/USD minor units without mixing currencies', () => {
    expect(formatBillingMinor('CNY', 1935)).toBe('¥19.35');
    expect(formatBillingMinor('USD', 100)).toBe('$1.00');
  });

  it('does not invent a decimal convention for unknown currencies', () => {
    expect(formatBillingMinor('JPY', 1935)).toBe('1935 minor JPY');
    expect(formatBillingMinor('', 0)).toBe('0 minor');
  });
});
