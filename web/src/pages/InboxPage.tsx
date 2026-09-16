import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Descriptions,
  Drawer,
  Popconfirm,
  Result,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import {
  CloudServerOutlined,
  DeleteOutlined,
  KeyOutlined,
  MailOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useAccounts, useFetch } from '../api/hooks'
import type { AliasListResult, FullMessage, InboxMessage, InboxResult } from '../api/types'
import MailBody from '../components/MailBody'
import { formatDateTime } from '../utils/format'

const DAY_OPTIONS = [
  { value: 1, label: '近 1 天' },
  { value: 3, label: '近 3 天' },
  { value: 7, label: '近 7 天' },
  { value: 30, label: '近 30 天' },
  { value: 90, label: '近 90 天' },
]

const LIMIT_OPTIONS = [
  { value: 20, label: '20 封' },
  { value: 50, label: '50 封' },
  { value: 100, label: '100 封' },
]

export default function InboxPage() {
  const { message } = AntdApp.useApp()
  const { accounts, loading: accountsLoading } = useAccounts()
  const [searchParams, setSearchParams] = useSearchParams()

  const [days, setDays] = useState(7)
  const [limit, setLimit] = useState(20)
  const [detail, setDetail] = useState<FullMessage | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [deleting, setDeleting] = useState(false)

  const queryAccount = searchParams.get('account_id') ?? ''
  const queryAlias = searchParams.get('alias') ?? ''

  const accountId = useMemo(() => {
    if (accounts.length === 0) return ''
    return accounts.some((a) => a.id === queryAccount) ? queryAccount : accounts[0].id
  }, [accounts, queryAccount])

  // 别名筛选直接来自 URL,不做本地镜像状态
  const alias = queryAlias

  // URL 是账号/别名筛选的唯一真相来源
  useEffect(() => {
    const next: Record<string, string> = {}
    if (accountId) next.account_id = accountId
    if (queryAlias) next.alias = queryAlias
    if (next.account_id !== queryAccount || next.alias !== queryAlias) {
      setSearchParams(next, { replace: true })
    }
  }, [accountId, queryAccount, queryAlias, setSearchParams])

  const aliasesUrl = accountId ? `/api/aliases?account_id=${encodeURIComponent(accountId)}` : null
  const { data: aliasData } = useFetch<AliasListResult>(aliasesUrl)

  const inboxUrl = useMemo(() => {
    if (!accountId) return null
    const params = new URLSearchParams({ account_id: accountId, limit: String(limit), days: String(days) })
    if (alias) params.set('alias', alias)
    return `/api/inbox?${params.toString()}`
  }, [accountId, alias, limit, days])

  const { data, loading, error, reload } = useFetch<InboxResult>(inboxUrl)

  async function openMessage(record: InboxMessage) {
    setDetailLoading(true)
    try {
      // sanitize=1:额外取一份清理后的 HTML,界面用这份渲染(原始版仍在 body_html 里)
      const full = await request<FullMessage>(
        `/api/inbox/${encodeURIComponent(record.id)}?account_id=${encodeURIComponent(accountId)}&sanitize=1`,
      )
      setDetail(full)
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '读取邮件详情失败')
    } finally {
      setDetailLoading(false)
    }
  }

  async function deleteMessage(target: InboxMessage | FullMessage) {
    setDeleting(true)
    try {
      await request(
        `/api/inbox/${encodeURIComponent(target.id)}?account_id=${encodeURIComponent(accountId)}`,
        { method: 'DELETE' },
      )
      setDetail(null)
      message.success('邮件已删除')
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '删除邮件失败')
    } finally {
      setDeleting(false)
    }
  }

  function updateAlias(next: string) {
    const params: Record<string, string> = { account_id: accountId }
    if (next) params.alias = next
    setSearchParams(params, { replace: true })
  }

  const columns: TableProps<InboxMessage>['columns'] = [
    {
      title: '主题',
      dataIndex: 'subject',
      render: (subject: string, record) => (
        <Button type="link" size="small" style={{ padding: 0 }} onClick={() => void openMessage(record)}>
          {subject || '（无主题）'}
        </Button>
      ),
    },
    {
      title: '发件人',
      dataIndex: 'from',
      width: 220,
      ellipsis: true,
    },
    {
      title: '收件别名',
      dataIndex: 'to',
      width: 220,
      ellipsis: true,
    },
    {
      title: '时间',
      dataIndex: 'date',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '摘要',
      dataIndex: 'preview',
      ellipsis: true,
      render: (preview: string) => preview || '—',
    },
    {
      title: '操作',
      key: 'actions',
      width: 100,
      render: (_, record) => (
        <Popconfirm
          title="删除该邮件？"
          description="邮件会从收件箱中永久删除。"
          okText="删除"
          okButtonProps={{ danger: true }}
          cancelText="取消"
          onConfirm={() => void deleteMessage(record)}
        >
          <Button type="link" size="small" danger icon={<DeleteOutlined />} loading={deleting}>
            删除
          </Button>
        </Popconfirm>
      ),
    },
  ]

  if (!accountsLoading && accounts.length === 0) {
    return (
      <Result
        status="info"
        title="还没有账号"
        subTitle="读取邮件前需要先添加 iCloud 账号，并配置 Cookie 或 App 专用密码。"
        extra={
          <Link to="/accounts">
            <Button type="primary">去添加账号</Button>
          </Link>
        }
      />
    )
  }

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h2>取件</h2>
          <p>读取发往 Hide My Email 别名的邮件，可按别名与时间范围筛选</p>
        </div>
        {data && (
          <Tag
            icon={data.method === 'imap' ? <KeyOutlined /> : <CloudServerOutlined />}
            color={data.method === 'imap' ? 'cyan' : 'default'}
          >
            读取方式：{data.method === 'imap' ? 'IMAP (App 密码)' : 'Web API (Cookie)'}
          </Tag>
        )}
      </div>

      <Card variant="borderless" style={{ marginBottom: 16 }}>
        <Space wrap align="end" size={12}>
          <div className="field">
            <label htmlFor="inbox-account">账号</label>
            <Select
              id="inbox-account"
              value={accountId || undefined}
              style={{ minWidth: 180 }}
              loading={accountsLoading}
              onChange={(value) => setSearchParams({ account_id: value }, { replace: true })}
              options={accounts.map((a) => ({ value: a.id, label: a.name }))}
            />
          </div>
          <div className="field">
            <label htmlFor="inbox-alias">别名</label>
            <Select
              id="inbox-alias"
              value={alias || undefined}
              allowClear
              showSearch
              style={{ minWidth: 260 }}
              placeholder="全部别名"
              onChange={(value) => updateAlias(value ?? '')}
              options={(aliasData?.aliases ?? []).map((a) => ({
                value: a.email,
                label: a.label ? `${a.email}（${a.label}）` : a.email,
              }))}
            />
          </div>
          <div className="field">
            <label htmlFor="inbox-days">时间范围</label>
            <Select id="inbox-days" value={days} style={{ width: 130 }} options={DAY_OPTIONS} onChange={setDays} />
          </div>
          <div className="field">
            <label htmlFor="inbox-limit">条数</label>
            <Select id="inbox-limit" value={limit} style={{ width: 110 }} options={LIMIT_OPTIONS} onChange={setLimit} />
          </div>
          <Button type="primary" icon={<ReloadOutlined />} onClick={reload} loading={loading}>
            刷新
          </Button>
        </Space>
      </Card>

      <Card
        variant="borderless"
        title={`收件箱${data ? ` · ${data.count} 封` : ''}`}
        extra={<MailOutlined style={{ color: '#94a3b8' }} />}
      >
        {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
        <Table<InboxMessage>
          rowKey="id"
          columns={columns}
          dataSource={data?.messages ?? []}
          loading={loading}
          pagination={{ pageSize: 20, hideOnSinglePage: true, showSizeChanger: false }}
          scroll={{ x: 900 }}
          locale={{ emptyText: '所选范围内没有邮件' }}
        />
      </Card>

      <Drawer
        open={detail !== null || detailLoading}
        size={Math.min(680, typeof window === 'undefined' ? 680 : window.innerWidth - 32)}
        title={detail?.subject || '邮件详情'}
        onClose={() => setDetail(null)}
        loading={detailLoading}
        extra={
          detail && (
            <Popconfirm
              title="删除该邮件？"
              okText="删除"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={() => void deleteMessage(detail)}
            >
              <Button danger size="small" icon={<DeleteOutlined />}>
                删除
              </Button>
            </Popconfirm>
          )
        }
      >
        {detail && (
          <>
            <Descriptions
              size="small"
              column={1}
              style={{ marginBottom: 16 }}
              items={[
                { key: 'from', label: '发件人', children: detail.from || '—' },
                { key: 'to', label: '收件人', children: detail.to || '—' },
                { key: 'date', label: '时间', children: formatDateTime(detail.date) },
                { key: 'type', label: '内容类型', children: detail.content_type || 'text/plain' },
              ]}
            />
            <MailBody key={detail.id} message={detail} />
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 12 }}>
              超长内容可在正文区域内滚动查看。
            </Typography.Paragraph>
          </>
        )}
      </Drawer>
    </div>
  )
}
