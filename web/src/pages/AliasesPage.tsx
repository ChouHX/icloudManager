import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Result,
  Segmented,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import {
  CheckCircleOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  InboxOutlined,
  PlusOutlined,
  ReloadOutlined,
  StopOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useAccounts, useFetch } from '../api/hooks'
import AutoCreatePanel from '../components/AutoCreatePanel'
import type { Alias, AliasListResult, CreateAliasResult } from '../api/types'
import { formatDateTime, timestampValue } from '../utils/format'

type StatusFilter = 'all' | 'active' | 'inactive'

export default function AliasesPage() {
  const { message, modal } = AntdApp.useApp()
  const { accounts, loading: accountsLoading, error: accountsError, reload: reloadAccounts } = useAccounts()
  const [searchParams, setSearchParams] = useSearchParams()

  const [createOpen, setCreateOpen] = useState(false)
  const [autoOpen, setAutoOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState('')
  const [created, setCreated] = useState<CreateAliasResult | null>(null)
  const [pendingId, setPendingId] = useState('')
  const [search, setSearch] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [form] = Form.useForm<{ label?: string }>()

  const queryAccount = searchParams.get('account_id') ?? ''
  const accountId = useMemo(() => {
    if (accounts.length === 0) return ''
    return accounts.some((a) => a.id === queryAccount) ? queryAccount : accounts[0].id
  }, [accounts, queryAccount])

  // 让 URL 始终反映当前账号，便于分享与刷新保持状态
  useEffect(() => {
    if (accountId && accountId !== queryAccount) {
      setSearchParams({ account_id: accountId }, { replace: true })
    }
  }, [accountId, queryAccount, setSearchParams])

  const aliasesUrl = accountId ? `/api/aliases?account_id=${encodeURIComponent(accountId)}` : null
  const { data, loading, error, reload } = useFetch<AliasListResult>(aliasesUrl)

  const rows = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return (data?.aliases ?? [])
      .filter((alias) => {
        if (status === 'active' && !alias.active) return false
        if (status === 'inactive' && alias.active) return false
        if (!keyword) return true
        return (
          alias.email.toLowerCase().includes(keyword) ||
          (alias.label ?? '').toLowerCase().includes(keyword)
        )
      })
      .sort((left, right) => {
        const a = timestampValue(left.createdAt)
        const b = timestampValue(right.createdAt)
        if (a === null || b === null) return a === b ? 0 : a === null ? 1 : -1
        return b - a
      })
  }, [data, search, status])

  async function handleCreate() {
    let values: { label?: string }
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setCreating(true)
    setCreateError('')
    try {
      const result = await request<CreateAliasResult>('/api/create', {
        method: 'POST',
        body: { account_id: accountId, label: values.label?.trim() ?? '' },
      })
      setCreateOpen(false)
      form.resetFields()
      setCreated(result)
      reload()
      reloadAccounts()
    } catch (err) {
      setCreateError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setCreating(false)
    }
  }

  async function runAction(alias: Alias, action: 'deactivate' | 'reactivate' | 'delete') {
    setPendingId(alias.anonymousId)
    try {
      if (action === 'delete') {
        await request(`/api/aliases/${encodeURIComponent(alias.anonymousId)}`, {
          method: 'DELETE',
          body: { account_id: accountId },
        })
        message.success('别名已删除')
      } else {
        await request(
          `/api/aliases/${encodeURIComponent(alias.anonymousId)}/${action}`,
          { method: 'POST', body: { account_id: accountId } },
        )
        message.success(action === 'deactivate' ? '别名已停用' : '别名已激活')
      }
      reload()
      reloadAccounts()
    } catch (err) {
      modal.error({
        title: '操作失败',
        content: err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态',
      })
    } finally {
      setPendingId('')
    }
  }

  const columns: TableProps<Alias>['columns'] = [
    {
      title: '别名邮箱',
      dataIndex: 'email',
      render: (email: string) => (
        <Typography.Text copyable={{ text: email }} className="mono">
          {email}
        </Typography.Text>
      ),
    },
    {
      title: '标签',
      dataIndex: 'label',
      render: (label: string) => label || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: '状态',
      dataIndex: 'active',
      width: 110,
      render: (active: boolean) =>
        active ? (
          <Tag icon={<CheckCircleOutlined />} color="success">
            已启用
          </Tag>
        ) : (
          <Tag icon={<ClockCircleOutlined />}>已停用</Tag>
        ),
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      width: 170,
      sorter: (a, b) => (timestampValue(a.createdAt) ?? 0) - (timestampValue(b.createdAt) ?? 0),
      defaultSortOrder: 'descend',
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 260,
      render: (_, alias) => (
        <Space size={4} wrap>
          <Link to={`/inbox?account_id=${encodeURIComponent(accountId)}&alias=${encodeURIComponent(alias.email)}`}>
            <Button type="link" size="small" icon={<InboxOutlined />}>
              取件
            </Button>
          </Link>
          <Popconfirm
            title={alias.active ? '停用该别名？' : '重新激活该别名？'}
            description={
              alias.active ? '停用后该邮箱将不再接收邮件，可随时重新激活。' : '激活后该邮箱恢复收件。'
            }
            okText="确认"
            cancelText="取消"
            onConfirm={() => void runAction(alias, alias.active ? 'deactivate' : 'reactivate')}
          >
            <Button
              type="link"
              size="small"
              icon={alias.active ? <StopOutlined /> : <CheckCircleOutlined />}
              loading={pendingId === alias.anonymousId}
            >
              {alias.active ? '停用' : '激活'}
            </Button>
          </Popconfirm>
          <Popconfirm
            title="删除该别名？"
            description="删除后在 iCloud 侧不可恢复，且该地址立即失效。"
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => void runAction(alias, 'delete')}
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  if (accountsError && accounts.length === 0) {
    return (
      <Result
        status="warning"
        title="读取账号失败"
        subTitle={accountsError}
        extra={<Button type="primary" onClick={reloadAccounts}>重试</Button>}
      />
    )
  }

  if (!accountsLoading && accounts.length === 0) {
    return (
      <Result
        status="info"
        title="还没有账号"
        subTitle="创建别名前需要先添加一个 iCloud 账号并配置 Cookie。"
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
          <h2>创建别名</h2>
          <p>一键生成新的 Hide My Email 地址，并管理已有别名</p>
        </div>
        <Space wrap>
          <Select
            value={accountId || undefined}
            style={{ minWidth: 180 }}
            loading={accountsLoading}
            onChange={(value) => setSearchParams({ account_id: value }, { replace: true })}
            options={accounts.map((a) => ({ value: a.id, label: a.name }))}
            aria-label="选择账号"
          />
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)} disabled={!accountId}>
            创建别名
          </Button>
          <Button icon={<ThunderboltOutlined />} onClick={() => setAutoOpen(true)} disabled={!accountId}>
            自动建满
          </Button>
        </Space>
      </div>

      <Card
        variant="borderless"
        title={`别名列表${data ? ` · ${data.count}` : ''}`}
        extra={
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索邮箱或标签"
              style={{ width: 220 }}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <Segmented
              value={status}
              onChange={(value) => setStatus(value as StatusFilter)}
              options={[
                { value: 'all', label: '全部' },
                { value: 'active', label: '已启用' },
                { value: 'inactive', label: '已停用' },
              ]}
            />
            <Button icon={<ReloadOutlined />} onClick={reload} aria-label="刷新列表" />
          </Space>
        }
      >
        {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
        <Table<Alias>
          rowKey={(alias) => alias.anonymousId || alias.email}
          columns={columns}
          dataSource={rows}
          loading={loading}
          pagination={{ pageSize: 20, hideOnSinglePage: true, showSizeChanger: false }}
          scroll={{ x: 860 }}
        />
      </Card>

      <AutoCreatePanel
        open={autoOpen}
        accountId={accountId}
        accounts={accounts}
        onClose={() => setAutoOpen(false)}
        onProgress={() => {
          reload()
          reloadAccounts()
        }}
      />

      <Modal
        open={createOpen}
        title="创建别名"
        onCancel={() => {
          setCreateOpen(false)
          setCreateError('')
        }}
        onOk={() => void handleCreate()}
        okText="创建"
        confirmLoading={creating}
        destroyOnHidden
        mask={{ closable: false }}
      >
        {createError && <Alert type="error" showIcon title={createError} style={{ marginBottom: 16 }} />}
        <Form form={form} layout="vertical" requiredMark="optional">
          <Form.Item
            name="label"
            label="标签（可选）"
            extra="用于备忘这个地址的用途，最长 200 字符"
            rules={[{ max: 200, message: '标签最长 200 字符' }]}
          >
            <Input placeholder="例如：购物、订阅" maxLength={200} />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        open={created !== null}
        title="别名创建成功"
        onCancel={() => setCreated(null)}
        footer={[
          <Button key="close" onClick={() => setCreated(null)}>
            关闭
          </Button>,
          <Link
            key="inbox"
            to={`/inbox?account_id=${encodeURIComponent(accountId)}&alias=${encodeURIComponent(created?.email ?? '')}`}
          >
            <Button type="primary" onClick={() => setCreated(null)}>
              去取件
            </Button>
          </Link>,
        ]}
        destroyOnHidden
      >
        {created && (
          <div className="created-address">
            <Typography.Text className="mono" copyable={{ text: created.email }}>
              {created.email}
            </Typography.Text>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              点击地址即可复制
            </Typography.Text>
          </div>
        )}
      </Modal>
    </div>
  )
}
