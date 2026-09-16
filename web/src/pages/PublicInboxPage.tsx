import { useState } from 'react'
import { Alert, Button, Card, Drawer, Empty, Select, Space, Spin, Table, Tag, Typography } from 'antd'
import type { TableProps } from 'antd'
import { CloudServerOutlined, InboxOutlined, KeyOutlined, ReloadOutlined } from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useFetch } from '../api/hooks'
import type { FullMessage, InboxMessage, ShareInboxResult } from '../api/types'
import MailBody from '../components/MailBody'
import { formatDateTime } from '../utils/format'

const DAY_OPTIONS = [
  { value: 1, label: '近 1 天' },
  { value: 7, label: '近 7 天' },
  { value: 30, label: '近 30 天' },
]

const LIMIT_OPTIONS = [
  { value: 20, label: '20 封' },
  { value: 50, label: '50 封' },
]

/**
 * 凭取件链接访问的独立界面。
 *
 * 只读:展示该 token 对应别名收到的邮件与正文,不涉及账号管理、
 * 不显示其它别名,也不提供删除等写操作。
 */
export default function PublicInboxPage({ token }: { token: string }) {
  const [days, setDays] = useState(7)
  const [limit, setLimit] = useState(20)
  const [detail, setDetail] = useState<FullMessage | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')

  const url = `/api/share/${encodeURIComponent(token)}/inbox?limit=${limit}&days=${days}`
  const { data, loading, error, reload } = useFetch<ShareInboxResult>(url)

  async function openMessage(record: InboxMessage) {
    setDetailLoading(true)
    setDetailError('')
    try {
      const full = await request<FullMessage>(
        `/api/share/${encodeURIComponent(token)}/inbox/${encodeURIComponent(record.id)}`,
      )
      setDetail(full)
    } catch (err) {
      setDetailError(err instanceof ApiError ? err.message : '读取邮件失败')
    } finally {
      setDetailLoading(false)
    }
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
    { title: '发件人', dataIndex: 'from', width: 220, ellipsis: true },
    {
      title: '时间',
      dataIndex: 'date',
      width: 170,
      render: (value: string) => formatDateTime(value),
    },
    { title: '摘要', dataIndex: 'preview', ellipsis: true, render: (preview: string) => preview || '—' },
  ]

  const invalid = error.includes('SHARE_INVALID') || error.includes('无效或已失效')

  return (
    <div className="page" style={{ maxWidth: 960, paddingTop: 32 }}>
      <div className="page-head">
        <div>
          <h2>隐私邮箱收件箱</h2>
          <p>
            凭取件链接查看 <code>{data?.alias ?? '…'}</code> 收到的邮件（只读）
          </p>
        </div>
        {data && (
          <Tag
            icon={data.method === 'imap' ? <KeyOutlined /> : <CloudServerOutlined />}
            color={data.method === 'imap' ? 'cyan' : 'default'}
          >
            {data.method === 'imap' ? 'IMAP' : 'Web API'}
          </Tag>
        )}
      </div>

      {invalid ? (
        <Card variant="borderless">
          <Empty
            image={<InboxOutlined style={{ fontSize: 48, color: '#94a3b8' }} />}
            description={
              <>
                <Typography.Title level={5} style={{ marginBottom: 4 }}>
                  取件链接无效或已失效
                </Typography.Title>
                <Typography.Text type="secondary">请向链接提供者索取新的取件链接。</Typography.Text>
              </>
            }
          />
        </Card>
      ) : (
        <Card
          variant="borderless"
          title={`收件箱${data ? ` · ${data.count} 封` : ''}`}
          extra={
            <Space>
              <Select value={days} style={{ width: 120 }} options={DAY_OPTIONS} onChange={setDays} />
              <Select value={limit} style={{ width: 100 }} options={LIMIT_OPTIONS} onChange={setLimit} />
              <Button icon={<ReloadOutlined />} onClick={reload} loading={loading} aria-label="刷新" />
            </Space>
          }
        >
          {error && !invalid && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
          {detailError && <Alert type="error" showIcon title={detailError} style={{ marginBottom: 16 }} />}
          {loading && !data ? (
            <div style={{ textAlign: 'center', padding: '32px 0' }}>
              <Spin tip="读取中…" />
            </div>
          ) : (
            <Table<InboxMessage>
              rowKey="id"
              columns={columns}
              dataSource={data?.messages ?? []}
              loading={loading}
              pagination={{ pageSize: 20, hideOnSinglePage: true, showSizeChanger: false }}
              scroll={{ x: 720 }}
              locale={{ emptyText: '所选范围内没有邮件' }}
            />
          )}
        </Card>
      )}

      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 16, textAlign: 'center' }}>
        这是一个只读页面：只能查看该别名收到的邮件，无法访问其它别名或账号设置。
      </Typography.Paragraph>

      <Drawer
        open={detail !== null || detailLoading}
        size={Math.min(680, typeof window === 'undefined' ? 680 : window.innerWidth - 32)}
        title={detail?.subject || '邮件详情'}
        onClose={() => setDetail(null)}
        loading={detailLoading}
      >
        {detail && <MailBody key={detail.id} message={detail} />}
      </Drawer>
    </div>
  )
}
