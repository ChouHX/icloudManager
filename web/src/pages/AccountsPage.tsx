import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Dropdown,
  Result,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import {
  ApiOutlined,
  CloudUploadOutlined,
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  InboxOutlined,
  KeyOutlined,
  MailOutlined,
  PlusOutlined,
  SafetyOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useAccounts } from '../api/hooks'
import type { AccountSummary } from '../api/types'
import { AccountFormModal, CredentialModal, ICloudLoginModal, MailboxModal } from '../components/AccountModals'

const STATUS_META: Record<string, { text: string; color: string }> = {
  active: { text: '正常', color: 'success' },
  pending: { text: '待配置', color: 'warning' },
  error: { text: '异常', color: 'error' },
}

export default function AccountsPage() {
  const { accounts, loading, error, reload } = useAccounts()
  const { message, modal } = AntdApp.useApp()

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<AccountSummary | null>(null)
  const [target, setTarget] = useState<AccountSummary | null>(null)
  const [mode, setMode] = useState<'cookies' | 'password' | 'proxy' | 'login' | 'mailbox' | null>(null)

  function openCredential(account: AccountSummary, next: typeof mode) {
    setTarget(account)
    setMode(next)
  }

  function closeCredential() {
    setMode(null)
    setTarget(null)
  }

  async function removeAccount(account: AccountSummary) {
    try {
      await request(`/api/accounts/${encodeURIComponent(account.id)}`, { method: 'DELETE' })
      message.success('账号已删除')
      reload()
    } catch (err) {
      modal.error({
        title: '删除失败',
        content: err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态',
      })
    }
  }

  const columns: TableProps<AccountSummary>['columns'] = [
    {
      title: '账号',
      dataIndex: 'name',
      render: (name: string, account) => (
        <>
          <Typography.Text strong>{name}</Typography.Text>
          {account.status_message && (
            <Typography.Paragraph type="secondary" style={{ margin: 0, fontSize: 12 }}>
              {account.status_message}
            </Typography.Paragraph>
          )}
          <Typography.Text type="secondary" className="mono" style={{ fontSize: 12 }}>
            {account.id}
          </Typography.Text>
        </>
      ),
    },
    {
      title: 'iCloud 邮箱',
      dataIndex: 'icloud_email',
      render: (value: string, account) => value || account.real_email || '—',
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: string) => {
        const meta = STATUS_META[status] ?? { text: status, color: 'default' }
        return <Tag color={meta.color}>{meta.text}</Tag>
      },
    },
    {
      title: '别名',
      key: 'aliases',
      width: 110,
      render: (_, account) => (
        <Typography.Text>
          {account.alias_active} / {account.alias_total}
        </Typography.Text>
      ),
    },
    {
      title: '凭据',
      key: 'credentials',
      width: 240,
      render: (_, account) => (
        <Space size={4} wrap>
          {account.has_cookies && <Tag color="cyan">Cookie</Tag>}
          {account.has_app_password && <Tag color="geekblue">App 密码</Tag>}
          {account.mailbox && <Tag color="purple">收件箱 {account.mailbox.email}</Tag>}
          {account.has_proxy && <Tag>代理</Tag>}
          {!account.has_cookies && !account.has_app_password && (
            <Typography.Text type="secondary">未配置</Typography.Text>
          )}
        </Space>
      ),
    },
    {
      title: '最近验证',
      dataIndex: 'last_validated',
      width: 170,
      render: (value: string) => (value ? value : '—'),
    },
    {
      title: '操作',
      key: 'actions',
      width: 210,
      render: (_, account) => (
        <Space size={4}>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => {
              setEditing(account)
              setFormOpen(true)
            }}
          >
            编辑
          </Button>
          <Link to={`/inbox?account_id=${encodeURIComponent(account.id)}`}>
            <Button type="link" size="small" icon={<InboxOutlined />}>
              取件
            </Button>
          </Link>
          <Dropdown
            trigger={['click']}
            menu={{
              items: [
                { key: 'cookies', icon: <SafetyOutlined />, label: '更新 Cookie' },
                { key: 'login', icon: <KeyOutlined />, label: 'iCloud 密码登录' },
                { key: 'password', icon: <MailOutlined />, label: '设置 App 专用密码' },
                { key: 'mailbox', icon: <CloudUploadOutlined />, label: '接入收件邮箱' },
                { key: 'proxy', icon: <ApiOutlined />, label: '设置代理' },
                { type: 'divider' },
                { key: 'delete', icon: <DeleteOutlined />, label: '删除账号', danger: true },
              ],
              onClick: ({ key }) => {
                if (key === 'delete') {
                  modal.confirm({
                    title: `删除账号「${account.name}」？`,
                    content: '只会移除本地配置，不会影响 Apple 账号本身。',
                    okText: '删除',
                    okButtonProps: { danger: true },
                    cancelText: '取消',
                    onOk: () => removeAccount(account),
                  })
                  return
                }
                openCredential(account, key as typeof mode)
              },
            }}
          >
            <Button type="link" size="small">
              凭据配置 <DownOutlined />
            </Button>
          </Dropdown>
        </Space>
      ),
    },
  ]

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h2>账号设置</h2>
          <p>管理 iCloud 账号与取件凭据（Cookie / App 专用密码 / 收件邮箱）</p>
        </div>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => {
            setEditing(null)
            setFormOpen(true)
          }}
        >
          添加账号
        </Button>
      </div>

      <Card variant="borderless">
        {error && (
          <Alert
            type="error"
            showIcon
            message={error}
            style={{ marginBottom: 16 }}
            action={<Button size="small" onClick={reload}>重试</Button>}
          />
        )}
        {!loading && accounts.length === 0 && !error ? (
          <Result
            status="info"
            title="还没有账号"
            subTitle="添加 iCloud 账号后即可创建别名并取件。"
            extra={
              <Button
                type="primary"
                onClick={() => {
                  setEditing(null)
                  setFormOpen(true)
                }}
              >
                添加账号
              </Button>
            }
          />
        ) : (
          <Table<AccountSummary>
            rowKey="id"
            columns={columns}
            dataSource={accounts}
            loading={loading}
            pagination={false}
            scroll={{ x: 1180 }}
          />
        )}
      </Card>

      <AccountFormModal
        open={formOpen}
        editing={editing}
        onClose={() => setFormOpen(false)}
        onSaved={() => {
          message.success(editing ? '账号已更新' : '账号已添加')
          reload()
        }}
      />

      {target && mode === 'cookies' && (
        <CredentialModal
          open
          title="更新 Cookie"
          description="粘贴 iCloud 网页版的 Cookie（请求头字符串或 JSON），有效期约 24 小时。"
          accountId={target.id}
          endpoint="/api/accounts/:id/cookies"
          method="PUT"
          fields={[{ name: 'cookies', label: 'Cookie', type: 'textarea', required: true, placeholder: 'a=1; b=2' }]}
          onClose={closeCredential}
          onSaved={() => {
            message.success('Cookie 已更新')
            reload()
          }}
        />
      )}

      {target && mode === 'password' && (
        <CredentialModal
          open
          title="设置 App 专用密码"
          description="App 专用密码用于 IMAP 取件，可在 appleid.apple.com 生成。"
          accountId={target.id}
          endpoint="/api/accounts/:id/password"
          method="POST"
          fields={[
            { name: 'icloud_email', label: '邮箱', required: true, initialValue: target.icloud_email || target.real_email },
            { name: 'app_password', label: 'App 专用密码', type: 'password', required: true },
          ]}
          onClose={closeCredential}
          onSaved={() => {
            message.success('App 专用密码已保存')
            reload()
          }}
        />
      )}

      {target && mode === 'proxy' && (
        <CredentialModal
          open
          title="设置代理"
          description="留空保存即可清除代理；出于安全考虑不回显当前值。"
          accountId={target.id}
          endpoint="/api/accounts/:id/proxy"
          method="PUT"
          fields={[{ name: 'proxy', label: '代理地址', placeholder: 'http://user:pass@host:port' }]}
          buildBody={(values) => ({ proxy: String(values.proxy ?? '').trim() })}
          onClose={closeCredential}
          onSaved={() => {
            message.success('代理已更新')
            reload()
          }}
        />
      )}

      {target && mode === 'login' && (
        <ICloudLoginModal
          open
          accountId={target.id}
          onClose={closeCredential}
          onSaved={() => {
            message.success('登录成功，Cookie 已更新')
            reload()
          }}
        />
      )}

      {target && mode === 'mailbox' && (
        <MailboxModal
          open
          accountId={target.id}
          current={target.mailbox}
          onClose={closeCredential}
          onSaved={() => {
            message.success('收件邮箱已接入')
            reload()
          }}
        />
      )}
    </div>
  )
}
