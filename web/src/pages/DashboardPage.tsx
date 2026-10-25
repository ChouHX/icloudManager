import { Alert, Button, Card, Col, Empty, Progress, Row, Space, Table, Tag, Typography } from 'antd'
import type { TableProps } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import { useDashboard } from '../api/hooks'
import type { AccountSummary } from '../api/types'
import { sharePercent } from '../utils/format'
import { MOCK_DASHBOARD_DATA } from './DashboardPage.mock'

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

/** iCloud 账号卡片:紧凑展示别名数量与活跃占比 */
function AccountCard({ account }: { account: AccountSummary }) {
  const percent =
    account.alias_total > 0
      ? Math.min(100, Math.round((account.alias_active / account.alias_total) * 100))
      : 0

  return (
    <Card
      size="small"
      variant="borderless"
      style={{ height: '100%' }}
      styles={{ body: { padding: 10 } }}
    >
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 6 }}>
        <Typography.Text strong ellipsis style={{ maxWidth: 140, fontSize: 13 }}>
          {account.name}
        </Typography.Text>
        <Tag
          color={STATUS_COLOR[account.status] ?? 'default'}
          style={{ marginInlineEnd: 0, fontSize: 11, lineHeight: '18px' }}
        >
          {STATUS_LABEL[account.status] ?? account.status}
        </Tag>
      </div>

      <Typography.Text
        type="secondary"
        className="mono"
        ellipsis
        copyable={account.icloud_email ? { text: account.icloud_email } : false}
        style={{ display: 'block', marginTop: 2, fontSize: 12 }}
      >
        {account.icloud_email || '—'}
      </Typography.Text>

      <div
        style={{
          display: 'flex',
          alignItems: 'baseline',
          gap: 12,
          marginTop: 6,
          fontSize: 12,
          color: '#64748b',
          whiteSpace: 'nowrap',
        }}
      >
        <span>
          别名{' '}
          <b style={{ color: '#1f2937', fontVariantNumeric: 'tabular-nums' }}>
            {account.alias_total}
          </b>
        </span>
        <span>
          活跃{' '}
          <b style={{ color: '#1f2937', fontVariantNumeric: 'tabular-nums' }}>
            {account.alias_active}
          </b>
        </span>
        <span style={{ marginLeft: 'auto', fontVariantNumeric: 'tabular-nums' }}>{percent}%</span>
      </div>

      <Progress
        percent={percent}
        showInfo={false}
        strokeWidth={4}
        style={{ marginTop: 4, marginBottom: 0 }}
      />
    </Card>
  )
}

interface UsageRow {
  key: string
  label: string
  count: number
}

/**
 * 网站 / 标签的使用记录数分布。
 *
 * 占比统一以「记录总数」为分母（一条记录可带多个标签，所以按标签统计的
 * 各占比之和会大于 100%，这是预期行为）。不要用「表内最大值」归一化，
 * 否则最大值那一行永远显示 100%，看起来像占满了整条进度条。
 */
function UsageTable({
  rows,
  labelTitle,
  total,
}: {
  rows: UsageRow[]
  labelTitle: string
  total: number
}) {
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
      title: '占记录总数',
      key: 'ratio',
      width: 220,
      render: (_, row) => {
        const percent = sharePercent(row.count, total)
        return <Progress percent={percent} size="small" format={() => `${percent}%`} />
      },
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
  // 本地调试:URL 带 ?mock=1 时使用内置 Mock 数据,不请求后端
  const mockMode =
    import.meta.env.DEV && new URLSearchParams(window.location.search).get('mock') === '1'
  const { data: fetched, loading, error, reload } = useDashboard(!mockMode)
  const data = mockMode ? MOCK_DASHBOARD_DATA : fetched

  const accounts = data?.accounts
  const accountItems = accounts?.items ?? []
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

      {mockMode && (
        <Alert
          type="info"
          showIcon
          message="当前为本地 Mock 数据（URL 带 ?mock=1），仅用于调试布局。"
          style={{ marginBottom: 16 }}
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
          {accountItems.length === 0 ? (
            <Empty description="暂无 iCloud 账号" />
          ) : (
            <Row gutter={[12, 12]}>
              {accountItems.map((account) => (
                <Col key={account.id} xs={24} sm={12} lg={8} xl={6}>
                  <AccountCard account={account} />
                </Col>
              ))}
            </Row>
          )}
        </Card>

        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title="按网站统计" variant="borderless" loading={loading && !data}>
              <UsageTable rows={siteRows} labelTitle="网站 / 服务" total={tags?.total ?? 0} />
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title="按标签统计" variant="borderless" loading={loading && !data}>
              <UsageTable rows={tagRows} labelTitle="项目标签" total={tags?.total ?? 0} />
            </Card>
          </Col>
        </Row>
      </Space>
    </div>
  )
}
