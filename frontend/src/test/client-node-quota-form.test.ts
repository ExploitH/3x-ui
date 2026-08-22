import { describe, expect, it } from 'vitest';

import {
  buildNodeQuotaRows,
  serializeNodeQuotaRows,
  type ClientNodeQuotaRow,
} from '@/pages/clients/clientNodeQuotaForm';
import type { ClientNodeQuotaView } from '@/hooks/useClients';
import type { NodeRecord } from '@/schemas/node';

const nodes: NodeRecord[] = [
  { id: 1, name: 'HK', enable: true },
  { id: 2, name: 'JP', enable: true },
];

function view(overrides: Partial<ClientNodeQuotaView>): ClientNodeQuotaView {
  return {
    nodeId: 1,
    nodeName: 'HK',
    totalBytes: 100 * 1024 * 1024 * 1024,
    resetPolicy: 'monthly',
    resetDay: 15,
    lastResetAt: 0,
    up: 30,
    down: 20,
    blocked: false,
    reason: '',
    blockedAt: 0,
    appliedAt: 0,
    lastError: '',
    updatedAt: 0,
    ...overrides,
  };
}

describe('per-node quota form data', () => {
  it('merges configured views with all known physical nodes', () => {
    const rows = buildNodeQuotaRows(nodes, [view({})]);

    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({
      nodeId: 1,
      nodeName: 'HK',
      totalGB: 100,
      resetPolicy: 'monthly',
      resetDay: 15,
      up: 30,
      down: 20,
    });
    expect(rows[1]).toMatchObject({
      nodeId: 2,
      nodeName: 'JP',
      totalGB: 0,
      resetPolicy: 'never',
      resetDay: 1,
      up: 0,
      down: 0,
    });
  });

  it('preserves exact byte values when the displayed GB value is unchanged', () => {
    const exactBytes = 100 * 1024 * 1024 * 1024 + 123;
    const rows: ClientNodeQuotaRow[] = buildNodeQuotaRows(nodes, [
      view({ totalBytes: exactBytes }),
    ]);
    const payload = serializeNodeQuotaRows(rows);

    expect(payload[0]).toMatchObject({
      nodeId: 1,
      totalBytes: exactBytes,
      resetPolicy: 'monthly',
      resetDay: 15,
    });
  });

  it('serializes a deliberately changed precise GB value instead of reusing old bytes', () => {
    const exactBytes = 100 * 1024 * 1024 * 1024 + 123;
    const rows = buildNodeQuotaRows(nodes, [view({ totalBytes: exactBytes })]);
    rows[0].totalGB = rows[0].totalGB - 0.000001;

    expect(serializeNodeQuotaRows(rows)[0].totalBytes).not.toBe(exactBytes);
  });

  it('serializes an unlimited row as zero bytes and keeps reset settings', () => {
    const rows = buildNodeQuotaRows(nodes, [
      view({ totalBytes: 0, resetPolicy: 'never', resetDay: 1 }),
    ]);
    rows[0].totalGB = 0;
    rows[0].resetPolicy = 'weekly';
    rows[0].resetDay = 7;

    expect(serializeNodeQuotaRows(rows)[0]).toEqual({
      nodeId: 1,
      totalBytes: 0,
      resetPolicy: 'weekly',
      resetDay: 7,
    });
  });
});
