import { useMemo, useState } from 'react'
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
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  ReloadOutlined,
  TagsOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useTags } from '../api/hooks'
import type { TagEntry, TagEntryInput } from '../api/types'
import { formatDateTime } from '../utils/format'
import { TagImportModal, TagManageModal } from '../components/TagModals'

/** 密码单元格:默认打码,可切换显示,支持一键复制 */
function PasswordCell({ password }: { password?: string }) {
  const [visible, setVisible] = useState(false)
  if (!password) {
    return <Typography.Text type="secondary">—</Typography.Text>
  }
  return (
    <Space size={4}>
      <Typography.Text className="mono" copyable={{ text: password }}>
        {visible ? password : '••••••••'}
      </Typography.Text>
      <Button
        type="link"
        size="small"
        onClick={() => setVisible((v) => !v)}
        aria-label={visible ? '隐藏密码' : '显示密码'}
      >
        {visible ? '隐藏' : '显示'}
      </Button>
    </Space>
  )
}

interface TagFormValues {
  email: string
  password?: string
  site?: string
  tags?: string[]
  note?: string
}

/** 新增 / 编辑标签凭据 */
function TagFormModal({
  open,
  editing,
  tagOptions,
  onClose,
  onSaved,
}: {
  open: boolean
  editing: TagEntry | null
  tagOptions: string[]
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<TagFormValues>()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const initialValues = editing
    ? {
        email: editing.email,
        password: editing.password ?? '',
        site: editing.site ?? '',
        tags: editing.tags ?? [],
        note: editing.note ?? '',
      }
    : { email: '', password: '', site: '', tags: [], note: '' }

  async function handleOk() {
    let values: TagFormValues
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    const body: TagEntryInput = {
      email: values.email.trim(),
      password: values.password ?? '',
      site: (values.site ?? '').trim(),
      tags: values.tags ?? [],
      note: (values.note ?? '').trim(),
    }
    setSubmitting(true)
    setError('')
    try {
      if (editing) {
        await request(`/api/tags/${encodeURIComponent(editing.id)}`, { method: 'PUT', body })
      } else {
        await request('/api/tags', { method: 'POST', body })
      }
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={editing ? '编辑标签账号' : '添加标签账号'}
      onCancel={() => {
        setError('')
        onClose()
      }}
      onOk={() => void handleOk()}
      okText="保存"
      confirmLoading={submitting}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Form
        form={form}
        initialValues={initialValues}
        preserve={false}
        layout="vertical"
        requiredMark="optional"
      >
        <Form.Item
          name="email"
          label="邮箱 / 账号"
          rules={[
            { required: true, message: '请填写邮箱' },
            { type: 'email', message: '邮箱格式不正确' },
          ]}
        >
          <Input placeholder="user@example.com" autoComplete="off" />
        </Form.Item>
        <Form.Item name="password" label="网站密码">
          <Input.Password placeholder="留空表示暂不记录密码" autoComplete="new-password" />
        </Form.Item>
        <Form.Item name="site" label="网站 / 服务" extra="例如 GitHub、Steam、某宝">
          <Input placeholder="GitHub" maxLength={100} />
        </Form.Item>
        <Form.Item
          name="tags"
          label="项目标签"
          extra="输入后回车即可新增，用于区分该账号归属的项目"
        >
          <Select
            mode="tags"
            placeholder="例如：项目A、游戏、测试"
            options={tagOptions.map((tag) => ({ value: tag, label: tag }))}
            tokenSeparators={[',', '，', ' ']}
            maxTagCount="responsive"
          />
        </Form.Item>
        <Form.Item name="note" label="备注">
          <Input.TextArea rows={3} maxLength={500} placeholder="可选的补充说明" />
        </Form.Item>
      </Form>
    </Modal>
  )
}

export default function TagsPage() {
  const { data, loading, error, reload } = useTags()
  const { message } = AntdApp.useApp()

  const [formOpen, setFormOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [manageOpen, setManageOpen] = useState(false)
  const [editing, setEditing] = useState<TagEntry | null>(null)
  const [search, setSearch] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])

  const entries = useMemo(() => data?.entries ?? [], [data])
  const tagOptions = useMemo(() => data?.tags ?? [], [data])

  const tagCounts = useMemo(() => {
    const map: Record<string, number> = {}
    for (const entry of entries) {
      for (const tag of entry.tags) {
        const key = tag.toLowerCase()
        map[key] = (map[key] ?? 0) + 1
      }
    }
    return map
  }, [entries])

  const rows = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return entries.filter((entry) => {
      if (selectedTags.length > 0) {
        const own = new Set(entry.tags.map((tag) => tag.toLowerCase()))
        if (!selectedTags.some((tag) => own.has(tag.toLowerCase()))) return false
      }
      if (!keyword) return true
      return (
        entry.email.toLowerCase().includes(keyword) ||
        (entry.site ?? '').toLowerCase().includes(keyword) ||
        (entry.note ?? '').toLowerCase().includes(keyword) ||
        entry.tags.some((tag) => tag.toLowerCase().includes(keyword))
      )
    })
  }, [entries, search, selectedTags])

  async function remove(entry: TagEntry) {
    try {
      await request(`/api/tags/${encodeURIComponent(entry.id)}`, { method: 'DELETE' })
      message.success('记录已删除')
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '删除失败，请重试')
    }
  }

  const columns: TableProps<TagEntry>['columns'] = [
    {
      title: '邮箱 / 账号',
      dataIndex: 'email',
      render: (email: string) => (
        <Typography.Text className="mono" copyable={{ text: email }}>
          {email}
        </Typography.Text>
      ),
    },
    {
      title: '网站 / 服务',
      dataIndex: 'site',
      width: 160,
      render: (site?: string) => site || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: '密码',
      dataIndex: 'password',
      width: 220,
      render: (password?: string) => <PasswordCell password={password} />,
    },
    {
      title: '项目标签',
      dataIndex: 'tags',
      render: (tags: string[]) =>
        tags.length > 0 ? (
          <Space size={4} wrap>
            {tags.map((tag) => (
              <Tag key={tag} color="blue">
                {tag}
              </Tag>
            ))}
          </Space>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        ),
    },
    {
      title: '备注',
      dataIndex: 'note',
      render: (note?: string) => note || <Typography.Text type="secondary">—</Typography.Text>,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      width: 170,
      render: (value?: string) => formatDateTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 150,
      render: (_, entry) => (
        <Space size={4}>
          <Button
            type="link"
            size="small"
            icon={<EditOutlined />}
            onClick={() => {
              setEditing(entry)
              setFormOpen(true)
            }}
          >
            编辑
          </Button>
          <Popconfirm
            title="删除该记录？"
            description="仅删除本地保存的标签与密码，不影响任何网站账号。"
            okText="删除"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => void remove(entry)}
          >
            <Button type="link" size="small" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h2>标签账号</h2>
          <p>记录邮箱、网站密码与项目标签，按项目区分账号使用情况（与 iCloud 账号无关）</p>
        </div>
        <Space wrap>
          <Button icon={<UploadOutlined />} onClick={() => setImportOpen(true)}>
            批量导入
          </Button>
          <Button icon={<TagsOutlined />} onClick={() => setManageOpen(true)}>
            标签管理
          </Button>
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              setEditing(null)
              setFormOpen(true)
            }}
          >
            添加记录
          </Button>
        </Space>
      </div>

      <Card
        variant="borderless"
        title={`记录列表${data ? ` · ${data.count}` : ''}`}
        extra={
          <Space wrap>
            <Input.Search
              allowClear
              placeholder="搜索邮箱、网站、备注或标签"
              style={{ width: 240 }}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <Select
              mode="multiple"
              allowClear
              placeholder="按项目标签筛选"
              style={{ minWidth: 200 }}
              value={selectedTags}
              onChange={setSelectedTags}
              options={tagOptions.map((tag) => ({ value: tag, label: tag }))}
              maxTagCount="responsive"
            />
            <Button icon={<ReloadOutlined />} onClick={reload} aria-label="刷新列表" />
          </Space>
        }
      >
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
        {!loading && entries.length === 0 && !error ? (
          <Result
            status="info"
            icon={<TagsOutlined />}
            title="还没有标签账号"
            subTitle="把邮箱、网站密码和项目标签记录下来，方便区分每个项目使用的账号。"
            extra={
              <Button
                type="primary"
                onClick={() => {
                  setEditing(null)
                  setFormOpen(true)
                }}
              >
                添加记录
              </Button>
            }
          />
        ) : (
          <Table<TagEntry>
            rowKey="id"
            columns={columns}
            dataSource={rows}
            loading={loading}
            pagination={{ pageSize: 20, hideOnSinglePage: true, showSizeChanger: false }}
            scroll={{ x: 1180 }}
          />
        )}
      </Card>

      <TagFormModal
        open={formOpen}
        editing={editing}
        tagOptions={tagOptions}
        onClose={() => setFormOpen(false)}
        onSaved={() => {
          message.success(editing ? '记录已更新' : '记录已添加')
          reload()
        }}
      />

      {importOpen && (
        <TagImportModal
          open
          tagOptions={tagOptions}
          onClose={() => setImportOpen(false)}
          onImported={() => reload()}
        />
      )}

      {manageOpen && (
        <TagManageModal
          open
          tags={tagOptions}
          counts={tagCounts}
          onClose={() => setManageOpen(false)}
          onChanged={() => reload()}
        />
      )}
    </div>
  )
}
