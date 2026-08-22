import type { ClientNodeQuotaInput, ClientNodeQuotaView } from '@/hooks/useClients';
import type { NodeRecord } from '@/schemas/node';

export interface ClientNodeQuotaRow {
  nodeId: number;
  nodeName: string;
  totalGB: number;
  originalTotalBytes: number;
  originalTotalGB: number;
  resetPolicy: string;
  resetDay: number;
  up: number;
  down: number;
  blocked: boolean;
  reason: string;
  appliedAt: number;
  lastError: string;
}

export function bytesToQuotaGB(bytes: number): number {
  if (!bytes || bytes <= 0) return 0;
  return bytes / (1024 * 1024 * 1024);
}

export function quotaGBToBytes(gb: number): number {
  if (!gb || gb <= 0) return 0;
  return Math.round(gb * 1024 * 1024 * 1024);
}

function rowFromView(
  nodeId: number,
  nodeName: string,
  current?: ClientNodeQuotaView,
): ClientNodeQuotaRow {
  const totalBytes = current?.totalBytes ?? 0;
  return {
    nodeId,
    nodeName: current?.nodeName || nodeName || `Node ${nodeId}`,
    totalGB: bytesToQuotaGB(totalBytes),
    originalTotalBytes: totalBytes,
    originalTotalGB: bytesToQuotaGB(totalBytes),
    resetPolicy: current?.resetPolicy || 'never',
    resetDay: current?.resetDay || 1,
    up: current?.up || 0,
    down: current?.down || 0,
    blocked: current?.blocked || false,
    reason: current?.reason || '',
    appliedAt: current?.appliedAt || 0,
    lastError: current?.lastError || '',
  };
}

export function buildNodeQuotaRows(
  nodes: NodeRecord[],
  views: ClientNodeQuotaView[],
): ClientNodeQuotaRow[] {
  const byNode = new Map(views.map((view) => [view.nodeId, view]));
  const rows = nodes.map((node) =>
    rowFromView(node.id, node.name || node.remark || '', byNode.get(node.id)),
  );
  const known = new Set(nodes.map((node) => node.id));
  for (const view of views) {
    if (!known.has(view.nodeId)) rows.push(rowFromView(view.nodeId, '', view));
  }
  return rows;
}

export function serializeNodeQuotaRows(rows: ClientNodeQuotaRow[]): ClientNodeQuotaInput[] {
  return rows.map((row) => ({
    nodeId: row.nodeId,
    totalBytes:
      row.originalTotalBytes > 0 && row.originalTotalGB === row.totalGB
        ? row.originalTotalBytes
        : quotaGBToBytes(Number(row.totalGB) || 0),
    resetPolicy: row.resetPolicy || 'never',
    resetDay: Number(row.resetDay) || 1,
  }));
}
