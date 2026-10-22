import { Alert, Button, Card, Col, Progress, Row, Space, Table, Tag, Typography } from 'antd'
import type { TableProps } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useDashboard } from '../api/hooks'
import type { AccountSummary } from '../api/types'

/** 顶部紧凑统计卡片 */
function StatItem({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="stat-item">
      <span className="stat-value">{value}</span>
      <span className="stat-label">{label}</span>
    </div>
  )
}

const STATUS_LABEL: Record<string, string> = {
  active: '正常',
  pending: '待配置',
  error: '异常',
}

const STATUS_COLOR: Record<string, string> = {
  active: 'green',
  pending: 'gold',
  error: 'red',
}

/** iCloud 账号及其别名数量 */
function AccountTable({ items }: { items: AccountSummary[] }) {
  const columns: TableProps<AccountSummary>['columns'] = [
    {
      title: '账号',
      dataIndex: 'name',
      render: (name: string, row) => (
        <Space size={6}>
          <Typography.Text strong>{name}</Typography.Text>
          <Tag color={STATUS_COLOR[row.status] ?? 'default'}>
            {STATUS_LABEL[row.status] ?? row.status}
          </Tag>
        </Space>
      ),
    },
    {
      title: 'iCloud 邮箱',
      dataIndex: 'icloud_email',
      render: (email?: string) =>
        email ? (
          <Typography.Text className="mono" copyable={{ text: email }}>
            {email}
          </Typography.Text>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      title: '别名总数',
      dataIndex: 'alias_total',
      width: 110,
      align: 'right',
    },
    {
      title: '活跃别名',
      dataIndex: 'alias_active',
      width: 110,
      align: 'right',
    },
    {
      title: '活跃占比',
      key: 'ratio',
      width: 180,
      render: (_, row) => {
        const percent =
          row.alias_total > 0
            ? Math.min(100, Math.round((row.alias_active / row.alias_total) * 100))
            : 0
        return <Progress percent={percent} size="small" />
      },
    },
  ]

  return (
    <Table<AccountSummary>
      rowKey="id"
      columns={columns}
      dataSource={items}
      pagination={false}
      size="small"
      scroll={{ x: 760 }}
      locale={{ emptyText: '暂无 iCloud 账号' }}
    />
  )
}

interface UsageRow {
  key: string
  label: string
  count: number
}

/** 网站 / 标签的使用记录数分布 */
function UsageTable({ rows, labelTitle }: { rows: UsageRow[]; labelTitle: string }) {
  const max = rows.reduce((acc, row) => Math.max(acc, row.count), 0)
  const columns: TableProps<UsageRow>['columns'] = [
    {
      title: labelTitle,
      dataIndex: 'label',
      render: (label: string) => <Tag color="blue">{label}</Tag>,
    },
    {
      title: '账号数',
      dataIndex: 'count',
      width: 100,
      align: 'right',
    },
    {
      title: '占比',
      key: 'ratio',
      width: 200,
      render: (_, row) => (
        <Progress percent={max > 0 ? Math.round((row.count / max) * 100) : 0} size="small" />
      ),
    },
  ]

  return (
    <Table<UsageRow>
      rowKey="key"
      columns={columns}
      dataSource={rows}
      pagination={false}
      size="small"
      locale={{ emptyText: '暂无数据' }}
    />
  )
}

/** 数据看板:汇总 iCloud 账号别名与标签凭据的网站/标签分布 */
export default function DashboardPage() {
  const { data, loading, error, reload } = useDashboard()

  const accounts = data?.accounts
  const tags = data?.tags
  const siteRows: UsageRow[] = (tags?.by_site ?? []).map((item) => ({
    key: item.site,
    label: item.site,
    count: item.count,
  }))
  const tagRows: UsageRow[] = (tags?.by_tag ?? []).map((item) => ({
    key: item.tag,
    label: item.tag,
    count: item.count,
  }))

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h2>数据看板</h2>
          <p>汇总 iCloud 账号的别名数量（最近一次检测结果），以及标签凭据的网站 / 标签分布。</p>
        </div>
        <Button icon={<ReloadOutlined />} onClick={reload} loading={loading}>
          刷新
        </Button>
      </div>

      {error && (
        <Alert
          type="error"
          showIcon
          message={error}
          style={{ marginBottom: 16 }}
          action={
            <Button size="small" onClick={reload}>
              重试
            </Button>
          }
        />
      )}

      <div className="stat-strip" style={{ marginBottom: 16 }}>
        <StatItem label="iCloud 账号" value={accounts?.total ?? 0} />
        <StatItem label="别名总数" value={accounts?.alias_total ?? 0} />
        <StatItem label="活跃别名" value={accounts?.alias_active ?? 0} />
        <StatItem label="标签账号" value={tags?.total ?? 0} />
        <StatItem label="已打标签" value={tags?.tagged ?? 0} />
        <StatItem label="未打标签" value={tags?.untagged ?? 0} />
        <StatItem label="标签种类" value={tags?.tag_kinds ?? 0} />
        <StatItem label="网站种类" value={tags?.site_kinds ?? 0} />
      </div>

      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Card title="iCloud 账号与别名" variant="borderless" loading={loading && !data}>
          <AccountTable items={accounts?.items ?? []} />
        </Card>

        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title="按网站统计" variant="borderless" loading={loading && !data}>
              <UsageTable rows={siteRows} labelTitle="网站 / 服务" />
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title="按标签统计" variant="borderless" loading={loading && !data}>
              <UsageTable rows={tagRows} labelTitle="项目标签" />
            </Card>
          </Col>
        </Row>
      </Space>
    </div>
  )
}
