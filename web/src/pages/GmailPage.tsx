import { useMemo, useState } from 'react'
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Drawer,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import {
  DeleteOutlined,
  DownloadOutlined,
  FilterOutlined,
  InboxOutlined,
  LinkOutlined,
  ReloadOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useFetch } from '../api/hooks'
import type {
  FullMessage,
  GmailAccount,
  GmailFilter,
  GmailImportResult,
  GmailListResult,
  GmailShareLink,
  GmailShareLinksResult,
  InboxMessage,
  ShareInboxResult,
} from '../api/types'
import MailBody from '../components/MailBody'
import { formatDateTime } from '../utils/format'

const DAY_OPTIONS = [
  { value: 1, label: '近 1 天' },
  { value: 7, label: '近 7 天' },
  { value: 30, label: '近 30 天' },
]

const IMPORT_HELP =
  '每行一条：账号----密码----2FA 密钥----应用专用密码。' +
  '缺少的中间字段留空即可（例如没有 2FA 就写成 账号----密码--------应用专用密码）；' +
  '只有账号与密码也可以。以 # 开头的行会被忽略。'

/** 触发浏览器下载一个文本文件 */
function downloadText(text: string, filename: string): void {
  const url = URL.createObjectURL(new Blob([text], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function describeFilter(filter: GmailFilter): string {
  const parts: string[] = []
  if (filter.include.length > 0) parts.push(`仅含 ${filter.include.join(' / ')}`)
  if (filter.exclude.length > 0) parts.push(`排除 ${filter.exclude.join(' / ')}`)
  if (filter.days > 0) parts.push(`近 ${filter.days} 天`)
  return parts.length > 0 ? parts.join('，') : '不限制'
}

/** Gmail 批量导入弹窗 */
function GmailImportModal({
  open,
  onClose,
  onImported,
}: {
  open: boolean
  onClose: () => void
  onImported: () => void
}) {
  const [form] = Form.useForm()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [result, setResult] = useState<GmailImportResult | null>(null)

  async function handleImport() {
    let values: { text?: string; separator?: string }
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const res = await request<GmailImportResult>('/api/gmail/import', {
        method: 'POST',
        body: { text: values.text ?? '', separator: (values.separator ?? '').trim() },
      })
      setResult(res)
      onImported()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title="批量导入 Gmail 账号"
      width={680}
      onCancel={onClose}
      destroyOnHidden
      mask={{ closable: false }}
      footer={
        result
          ? [
              <Button key="again" onClick={() => { setResult(null); setError(''); form.resetFields() }}>
                再导入一批
              </Button>,
              <Button key="close" type="primary" onClick={onClose}>
                完成
              </Button>,
            ]
          : [
              <Button key="cancel" onClick={onClose}>
                取消
              </Button>,
              <Button key="ok" type="primary" loading={submitting} onClick={() => void handleImport()}>
                开始导入
              </Button>,
            ]
      }
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      {result ? (
        <Space orientation="vertical" style={{ width: '100%' }} size={12}>
          <Alert
            type={result.invalid > 0 ? 'warning' : 'success'}
            showIcon
            message="导入完成"
            description={
              <Space size={16} wrap>
                <span>新增 <strong>{result.imported}</strong></span>
                <span>更新 <strong>{result.updated}</strong></span>
                <span>无效 <strong>{result.invalid}</strong></span>
              </Space>
            }
          />
          {result.errors.length > 0 && (
            <Alert
              type="warning"
              showIcon
              message="以下行未能导入"
              description={
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {result.errors.map((item) => (
                    <li key={item}>
                      <Typography.Text type="secondary">{item}</Typography.Text>
                    </li>
                  ))}
                </ul>
              }
            />
          )}
        </Space>
      ) : (
        <Form form={form} layout="vertical" requiredMark="optional">
          <Form.Item
            name="text"
            label="账号列表"
            extra={IMPORT_HELP}
            rules={[{ required: true, message: '请粘贴要导入的账号' }]}
          >
            <Input.TextArea
              rows={9}
              spellCheck={false}
              placeholder={'a@gmail.com----登录密码----JBSWY3DPEHPK3PXP----应用专用密码\nb@gmail.com----登录密码--------应用专用密码'}
            />
          </Form.Item>
          <Form.Item name="separator" label="自定义分隔符" extra="留空按 ---- 自动识别，制表符可填 \t">
            <Input placeholder="可留空" maxLength={8} spellCheck={false} />
          </Form.Item>
        </Form>
      )}
    </Modal>
  )
}

/** 全局取件过滤设置（对所有 Gmail 邮箱生效） */
function GmailFilterModal({
  filter,
  onClose,
  onSaved,
}: {
  filter: GmailFilter | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<GmailFilter>()
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  async function handleSave() {
    if (!filter) return
    let values: GmailFilter
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSaving(true)
    setError('')
    try {
      await request('/api/gmail/filter', {
        method: 'PUT',
        body: {
          include: values.include ?? [],
          exclude: values.exclude ?? [],
          days: values.days ?? 0,
        },
      })
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '保存失败，请重试')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      open={filter !== null}
      title="全局取件过滤（对所有 Gmail 邮箱生效）"
      width={560}
      onCancel={onClose}
      onOk={() => void handleSave()}
      okText="保存"
      confirmLoading={saving}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Form
        form={form}
        layout="vertical"
        preserve={false}
        initialValues={
          filter
            ? { include: filter.include, exclude: filter.exclude, days: filter.days }
            : { include: [], exclude: [], days: 0 }
        }
      >
        <Form.Item
          name="include"
          label="只显示包含这些关键词的邮件"
          extra="匹配主题或发件人，忽略大小写；留空表示不限制"
        >
          <Select mode="tags" placeholder="例如：验证码、noreply" tokenSeparators={[',', '，']} />
        </Form.Item>
        <Form.Item name="exclude" label="排除包含这些关键词的邮件" extra="优先级高于上面的包含规则">
          <Select mode="tags" placeholder="例如：广告、推广" tokenSeparators={[',', '，']} />
        </Form.Item>
        <Form.Item name="days" label="时间窗口（天）" extra="0 表示不限制，最多 90 天">
          <InputNumber min={0} max={90} style={{ width: 160 }} />
        </Form.Item>
      </Form>
    </Modal>
  )
}

/** 管理员预览：应用与取件链接相同的过滤条件 */
function GmailInboxDrawer({
  account,
  filter,
  onClose,
}: {
  account: GmailAccount | null
  filter: GmailFilter
  onClose: () => void
}) {
  const [days, setDays] = useState(7)
  const [limit, setLimit] = useState(20)
  const [detail, setDetail] = useState<FullMessage | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState('')

  const url = account
    ? `/api/gmail/${encodeURIComponent(account.id)}/inbox?limit=${limit}&days=${days}`
    : null
  const { data, loading, error, reload } = useFetch<ShareInboxResult>(url)

  async function openMessage(record: InboxMessage) {
    if (!account) return
    setDetailLoading(true)
    setDetailError('')
    try {
      const folder = record.folder ? `?folder=${encodeURIComponent(record.folder)}` : ''
      const full = await request<FullMessage>(
        `/api/gmail/${encodeURIComponent(account.id)}/inbox/${encodeURIComponent(record.id)}${folder}`,
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
      ellipsis: true,
      render: (subject: string, record) => (
        <>
          {record.folder && record.folder !== 'INBOX' && (
            <Tag color="orange" style={{ marginInlineEnd: 6 }}>
              垃圾邮件
            </Tag>
          )}
          <Button type="link" size="small" style={{ padding: 0 }} onClick={() => void openMessage(record)}>
            {subject || '（无主题）'}
          </Button>
        </>
      ),
    },
    { title: '发件人', dataIndex: 'from', width: 200, ellipsis: true },
    { title: '时间', dataIndex: 'date', width: 150, render: (value: string) => formatDateTime(value) },
  ]

  return (
    <Drawer
      open={account !== null}
      size={Math.min(760, typeof window === 'undefined' ? 760 : window.innerWidth - 32)}
      title={account ? `收件箱预览 · ${account.email}` : '收件箱预览'}
      onClose={() => {
        setDetail(null)
        onClose()
      }}
      extra={
        <Space>
          <Select value={days} style={{ width: 110 }} options={DAY_OPTIONS} onChange={setDays} />
          <Select
            value={limit}
            style={{ width: 90 }}
            options={[
              { value: 20, label: '20 封' },
              { value: 50, label: '50 封' },
            ]}
            onChange={setLimit}
          />
          <Button icon={<ReloadOutlined />} onClick={reload} loading={loading} aria-label="刷新" />
        </Space>
      }
    >
      {account && (
        <Typography.Paragraph type="secondary" style={{ fontSize: 12 }}>
          全局过滤条件：{describeFilter(filter)}
        </Typography.Paragraph>
      )}
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}
      {detailError && <Alert type="error" showIcon title={detailError} style={{ marginBottom: 12 }} />}
      {loading && !data ? (
        <div style={{ textAlign: 'center', padding: '24px 0' }}>
          <Spin tip="读取中…" />
        </div>
      ) : (
        <Table<InboxMessage>
          rowKey={(record) => `${record.folder ?? 'INBOX'}:${record.id}`}
          size="small"
          columns={columns}
          dataSource={data?.messages ?? []}
          pagination={{ pageSize: 20, hideOnSinglePage: true, showSizeChanger: false }}
          locale={{ emptyText: '所选范围内没有符合条件的邮件' }}
        />
      )}
      {detail && (
        <Card size="small" title={detail.subject || '邮件详情'} style={{ marginTop: 16 }}>
          <MailBody key={detail.id} message={detail} />
        </Card>
      )}
      {detailLoading && (
        <div style={{ textAlign: 'center', padding: 16 }}>
          <Spin size="small" />
        </div>
      )}
    </Drawer>
  )
}

/** Gmail 账号管理 + 取件链接 */
export default function GmailPage() {
  const { message } = AntdApp.useApp()
  const { data, loading, error, reload } = useFetch<GmailListResult>('/api/gmail')
  const [importOpen, setImportOpen] = useState(false)
  const [filterOpen, setFilterOpen] = useState(false)
  const [previewTarget, setPreviewTarget] = useState<GmailAccount | null>(null)
  const [busyId, setBusyId] = useState('')
  const [exporting, setExporting] = useState(false)

  const accounts = useMemo(() => data?.accounts ?? [], [data])
  const globalFilter = useMemo<GmailFilter>(
    () => data?.filter ?? { include: [], exclude: [], days: 0 },
    [data],
  )

  async function handleShare(account: GmailAccount) {
    setBusyId(account.id)
    try {
      const link = await request<GmailShareLink>(
        `/api/gmail/${encodeURIComponent(account.id)}/share-link`,
        { method: 'POST' },
      )
      try {
        await navigator.clipboard.writeText(link.url)
        message.success('取件链接已生成并复制到剪贴板')
      } catch {
        message.success('取件链接已生成')
        Modal.info({ title: '取件链接', content: link.url, okText: '知道了' })
      }
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '生成取件链接失败')
    } finally {
      setBusyId('')
    }
  }

  async function handleExportLinks() {
    setExporting(true)
    try {
      const res = await request<GmailShareLinksResult>('/api/gmail/share-links', { method: 'POST' })
      downloadText(res.text, `gmail-share-links-${new Date().toISOString().slice(0, 10)}.txt`)
      message.success(`已导出 ${res.count} 条取件链接`)
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '导出取件链接失败')
    } finally {
      setExporting(false)
    }
  }

  async function handleRevoke(account: GmailAccount) {
    setBusyId(account.id)
    try {
      await request(`/api/gmail/${encodeURIComponent(account.id)}/share-link`, { method: 'DELETE' })
      message.success('取件链接已撤销')
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '撤销失败')
    } finally {
      setBusyId('')
    }
  }

  async function handleCheck(account: GmailAccount) {
    setBusyId(account.id)
    try {
      await request(`/api/gmail/${encodeURIComponent(account.id)}/check`, { method: 'POST' })
      message.success('应用专用密码可用')
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '登录 Gmail 失败')
      reload()
    } finally {
      setBusyId('')
    }
  }

  async function handleDelete(account: GmailAccount) {
    setBusyId(account.id)
    try {
      await request(`/api/gmail/${encodeURIComponent(account.id)}`, { method: 'DELETE' })
      message.success('已删除')
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '删除失败')
    } finally {
      setBusyId('')
    }
  }

  const columns: TableProps<GmailAccount>['columns'] = [
    {
      title: 'Gmail 账号',
      dataIndex: 'email',
      render: (email: string, record) => (
        <Space orientation="vertical" size={0}>
          <Typography.Text className="mono" copyable={{ text: email }}>
            {email}
          </Typography.Text>
          <Space size={4} wrap>
            <Tag color={record.has_app_password ? 'green' : 'gold'}>
              {record.has_app_password ? '已配应用密码' : '缺应用密码'}
            </Tag>
            {record.has_totp && <Tag>2FA</Tag>}
            {record.status === 'error' && <Tag color="red">异常</Tag>}
          </Space>
        </Space>
      ),
    },
    {
      title: '取件链接',
      dataIndex: 'share_token',
      width: 200,
      render: (token: string | undefined, record: GmailAccount) =>
        token ? (
          <Space size={4} wrap>
            <Tag color="blue" icon={<LinkOutlined />}>
              已生成
            </Tag>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              访问 {record.share_hits ?? 0} 次
            </Typography.Text>
          </Space>
        ) : (
          <Typography.Text type="secondary">未生成</Typography.Text>
        ),
    },
    {
      title: '最近检测',
      dataIndex: 'last_checked',
      width: 160,
      render: (value: string | undefined, record: GmailAccount) => (
        <Tooltip title={record.last_error || ''}>
          <Typography.Text type={record.status === 'error' ? 'danger' : undefined} style={{ fontSize: 12 }}>
            {value ? formatDateTime(value) : '—'}
          </Typography.Text>
        </Tooltip>
      ),
    },
    {
      title: '操作',
      key: 'actions',
      width: 320,
      render: (_, record) => (
        <Space size={4} wrap>
          <Button size="small" icon={<InboxOutlined />} onClick={() => setPreviewTarget(record)}>
            预览
          </Button>
          {record.share_token ? (
            <Button size="small" danger loading={busyId === record.id} onClick={() => void handleRevoke(record)}>
              撤销链接
            </Button>
          ) : (
            <Button size="small" type="primary" loading={busyId === record.id} onClick={() => void handleShare(record)}>
              生成链接
            </Button>
          )}
          <Button size="small" loading={busyId === record.id} onClick={() => void handleCheck(record)}>
            检测
          </Button>
          <Button
            size="small"
            danger
            icon={<DeleteOutlined />}
            loading={busyId === record.id}
            onClick={() =>
              Modal.confirm({
                title: '删除该 Gmail 账号？',
                content: '删除后其取件链接会立即失效。',
                okText: '删除',
                okButtonProps: { danger: true },
                cancelText: '取消',
                onOk: () => void handleDelete(record),
              })
            }
          />
        </Space>
      ),
    },
  ]

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h2>Gmail 取件</h2>
          <p>
            导入 Gmail 账号后可为每个邮箱生成取件链接，未登录用户凭链接只能看到
            <strong>符合过滤条件</strong>的邮件。
          </p>
        </div>
        <Space wrap>
          <Button icon={<FilterOutlined />} onClick={() => setFilterOpen(true)}>
            全局过滤
          </Button>
          <Button
            icon={<DownloadOutlined />}
            loading={exporting}
            disabled={accounts.length === 0}
            onClick={() => void handleExportLinks()}
          >
            导出全部链接
          </Button>
          <Button icon={<ReloadOutlined />} onClick={reload} loading={loading}>
            刷新
          </Button>
          <Button type="primary" icon={<UploadOutlined />} onClick={() => setImportOpen(true)}>
            批量导入
          </Button>
        </Space>
      </div>

      {error && (
        <Alert
          type="error"
          showIcon
          title={error}
          style={{ marginBottom: 16 }}
          action={
            <Button size="small" onClick={reload}>
              重试
            </Button>
          }
        />
      )}

      <Card
        variant="borderless"
        title={`账号列表 · 全局过滤：${describeFilter(globalFilter)}`}
      >
        {accounts.length === 0 && !loading ? (
          <Empty description="还没有 Gmail 账号，先批量导入一批" />
        ) : (
          <Table<GmailAccount>
            rowKey="id"
            columns={columns}
            dataSource={accounts}
            loading={loading}
            pagination={{ pageSize: 20, hideOnSinglePage: true, showSizeChanger: false }}
            scroll={{ x: 1080 }}
          />
        )}
      </Card>

      <GmailImportModal
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onImported={reload}
      />
      <GmailFilterModal
        filter={filterOpen ? globalFilter : null}
        onClose={() => setFilterOpen(false)}
        onSaved={() => {
          message.success('全局取件过滤已保存')
          reload()
        }}
      />
      <GmailInboxDrawer
        account={previewTarget}
        filter={globalFilter}
        onClose={() => setPreviewTarget(null)}
      />

      <Typography.Paragraph type="secondary" style={{ fontSize: 12, marginTop: 16 }}>
        取件链接格式：<code>/?gtoken=…</code>，只能读取该邮箱过滤后的邮件，无法访问账号设置或其它邮箱。
      </Typography.Paragraph>
    </div>
  )
}
