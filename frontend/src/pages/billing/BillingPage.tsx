import { useQuery } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Layout,
  Row,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { DollarOutlined, ReloadOutlined } from '@ant-design/icons';

import AppSidebar from '@/layouts/AppSidebar';
import { HttpUtil } from '@/utils';
import { formatBillingMinor } from './billingFormat';

interface BillingCurrencySummary {
  baseMinor: number;
  overageMinor: number;
  totalMinor: number;
}

interface BillingLine {
  key: string;
  category: string;
  name: string;
  currency: string;
  baseMinor: number;
  overageMinor: number;
  totalMinor: number;
  source: string;
}

interface BillingSummary {
  asOf: number;
  byCurrency: Record<string, BillingCurrencySummary>;
  lines: BillingLine[];
}

function isBillingSummary(value: unknown): value is BillingSummary {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Partial<BillingSummary>;
  return (
    typeof candidate.asOf === 'number' &&
    !!candidate.byCurrency &&
    typeof candidate.byCurrency === 'object' &&
    Array.isArray(candidate.lines)
  );
}

async function fetchBillingSummary(): Promise<BillingSummary> {
  const msg = await HttpUtil.get('/panel/api/billing/summary', undefined, { silent: true });
  if (!msg?.success || !isBillingSummary(msg.obj)) {
    throw new Error(msg?.msg || 'Failed to fetch billing summary');
  }
  return msg.obj;
}

const lineColumns: ColumnsType<BillingLine> = [
  { title: 'Category', dataIndex: 'category', key: 'category' },
  { title: 'Name', dataIndex: 'name', key: 'name' },
  {
    title: 'Currency',
    dataIndex: 'currency',
    key: 'currency',
    render: (currency: string) => <Tag>{currency || '—'}</Tag>,
  },
  {
    title: 'Base',
    dataIndex: 'baseMinor',
    key: 'baseMinor',
    render: (value: number, row) => formatBillingMinor(row.currency, value),
  },
  {
    title: 'Overage',
    dataIndex: 'overageMinor',
    key: 'overageMinor',
    render: (value: number, row) => formatBillingMinor(row.currency, value),
  },
  {
    title: 'Total',
    dataIndex: 'totalMinor',
    key: 'totalMinor',
    render: (value: number, row) => formatBillingMinor(row.currency, value),
  },
  { title: 'Source', dataIndex: 'source', key: 'source' },
];

export default function BillingPage() {
  const query = useQuery({
    queryKey: ['billing', 'summary'],
    queryFn: fetchBillingSummary,
    staleTime: 30_000,
  });
  const summary = query.data ?? null;
  const loading = query.isFetching;
  const error = query.error instanceof Error ? query.error.message : '';

  const currencies = summary ? Object.entries(summary.byCurrency) : [];

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <AppSidebar />
      <Layout.Content style={{ padding: 24 }}>
        <div style={{ maxWidth: 1440, margin: '0 auto' }}>
          <Row justify="space-between" align="middle" style={{ marginBottom: 16 }}>
            <Col>
              <Typography.Title level={2} style={{ margin: 0 }}>
                Billing
              </Typography.Title>
              <Typography.Text type="secondary">
                Physical-node costs and provider/NIC reconciliation. Provider imports are explicit.
              </Typography.Text>
            </Col>
            <Col>
              <Button
                icon={<ReloadOutlined />}
                loading={loading}
                onClick={() => void query.refetch()}
              >
                Refresh
              </Button>
            </Col>
          </Row>

          {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 16 }} />}
          {loading && !summary ? (
            <div style={{ display: 'flex', justifyContent: 'center', padding: 80 }}>
              <Spin size="large" />
            </div>
          ) : summary ? (
            <>
              <Row gutter={[16, 16]} style={{ marginBottom: 16 }}>
                {currencies.length === 0 ? (
                  <Col span={24}>
                    <Alert
                      type="info"
                      showIcon
                      message="No cost records yet"
                      description="Add node cost profiles or infrastructure costs before enabling provider imports."
                    />
                  </Col>
                ) : (
                  currencies.map(([currency, values]) => (
                    <Col xs={24} sm={12} lg={8} key={currency}>
                      <Card size="small">
                        <Statistic
                          title={`${currency || 'Unknown'} monthly total`}
                          value={formatBillingMinor(currency, values.totalMinor)}
                          prefix={<DollarOutlined />}
                        />
                        <Typography.Text type="secondary">
                          Base {formatBillingMinor(currency, values.baseMinor)} · Overage{' '}
                          {formatBillingMinor(currency, values.overageMinor)}
                        </Typography.Text>
                      </Card>
                    </Col>
                  ))
                )}
              </Row>
              <Card
                title="Cost lines"
                extra={
                  <Typography.Text type="secondary">
                    As of {new Date(summary.asOf).toLocaleString()}
                  </Typography.Text>
                }
              >
                <Table<BillingLine>
                  rowKey={(row) => row.key}
                  size="small"
                  pagination={{ pageSize: 25 }}
                  columns={lineColumns}
                  dataSource={summary.lines}
                />
              </Card>
            </>
          ) : null}
        </div>
      </Layout.Content>
    </Layout>
  );
}
