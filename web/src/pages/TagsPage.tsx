import { useEffect, useMemo, useState } from 'react'
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
  Tooltip,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import {
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  InfoCircleOutlined,
  PlusOutlined,
  ReloadOutlined,
  TagsOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { useTags } from '../api/hooks'
import type { TagEntry, TagEntryInput } from '../api/types'
import { formatDateTime } from '../utils/format'
import { TagBackupModal, TagImportModal, TagManageModal } from '../components/TagModals'

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

/** 顶部紧凑统计卡片 */
function StatItem({ label, value }: { label: string; value: number }) {
  return (
    <div className="stat-item">
      <span className="stat-value">{value}</span>
      <span className="stat-label">{label}</span>
    </div>
  )
}

export default function TagsPage() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])

  // 输入防抖,避免每敲一个字符都请求服务端
  useEffect(() => {
    const timer = setTimeout(() => {
      setSearch(searchInput.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(timer)
  }, [searchInput])

  const query = useMemo(() => {
    const params = new URLSearchParams()
    params.set('page', String(page))
    params.set('page_size', String(pageSize))
    if (search) params.set('search', search)
    if (selectedTags.length > 0) params.set('tags', selectedTags.join(','))
    return `?${params.toString()}`
  }, [page, pageSize, search, selectedTags])

  const { data, loading, error, reload } = useTags(query)
  const { message } = AntdApp.useApp()

  const [formOpen, setFormOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [manageOpen, setManageOpen] = useState(false)
  const [backupOpen, setBackupOpen] = useState(false)
  const [editing, setEditing] = useState<TagEntry | null>(null)

  const entries = useMemo(() => data?.entries ?? [], [data])
  const tagOptions = useMemo(() => data?.tags ?? [], [data])
  const stats = data?.stats ?? { total: 0, tags: 0, sites: 0, untagged: 0 }
  const hasFilters = search !== '' || selectedTags.length > 0
  // 服务端可能因删除/筛选回退页码,展示时以服务端返回的页码为准
  const currentPage = data?.page ?? page

  // 标签使用数优先取服务端聚合结果(覆盖所有分页),缺失时才用当前页兜底
  const tagCounts = useMemo(() => {
    const map: Record<string, number> = {}
    const counts = data?.tag_counts
    if (counts && counts.length > 0) {
      for (const usage of counts) {
        map[usage.tag.toLowerCase()] = usage.count
      }
      return map
    }
    for (const entry of entries) {
      for (const tag of entry.tags ?? []) {
        const key = tag.toLowerCase()
        map[key] = (map[key] ?? 0) + 1
      }
    }
    return map
  }, [data, entries])

  async function remove(entry: TagEntry) {
    try {
      await request(`/api/tags/${encodeURIComponent(entry.id)}`, { method: 'DELETE' })
      message.success('记录已删除')
      reload()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '删除失败，请重试')
    }
  }

  function handlePageChange(nextPage: number, nextSize: number) {
    if (nextSize !== pageSize) {
      setPageSize(nextSize)
      setPage(1)
      return
    }
    setPage(nextPage)
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
      render: (tags?: string[]) => {
        const list = tags ?? []
        return list.length > 0 ? (
          <Space size={4} wrap>
            {list.map((tag) => (
              <Tag key={tag} color="blue">
                {tag}
              </Tag>
            ))}
          </Space>
        ) : (
          <Typography.Text type="secondary">—</Typography.Text>
        )
      },
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
        <div className="stat-strip">
          <Tooltip title="记录邮箱、网站密码与项目标签，用于区分每个项目使用的账号（与 iCloud 账号无关）">
            <Button
              type="text"
              size="small"
              shape="circle"
              className="stat-info"
              icon={<InfoCircleOutlined />}
              aria-label="标签账号说明"
            />
          </Tooltip>
          <StatItem label="记录" value={stats.total} />
          <StatItem label="标签" value={stats.tags} />
          <StatItem label="网站" value={stats.sites} />
          <StatItem label="未打标签" value={stats.untagged} />
        </div>
        <Space wrap>
          <Button icon={<UploadOutlined />} onClick={() => setImportOpen(true)}>
            批量导入
          </Button>
          <Button icon={<TagsOutlined />} onClick={() => setManageOpen(true)}>
            标签管理
          </Button>
          <Button icon={<DownloadOutlined />} onClick={() => setBackupOpen(true)}>
            备份迁移
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

      <Card variant="borderless">
        <div className="list-toolbar">
          <Input.Search
            allowClear
            placeholder="搜索邮箱、网站、备注或标签"
            style={{ width: 260 }}
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
          />
          <Select
            mode="multiple"
            allowClear
            placeholder="按项目标签筛选"
            style={{ minWidth: 220 }}
            value={selectedTags}
            onChange={(values) => {
              setSelectedTags(values)
              setPage(1)
            }}
            options={tagOptions.map((tag) => ({ value: tag, label: tag }))}
            maxTagCount="responsive"
          />
          <Button icon={<ReloadOutlined />} onClick={reload} aria-label="刷新列表" />
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
        {!loading && !error && (data?.count ?? 0) === 0 && !hasFilters ? (
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
            dataSource={entries}
            loading={loading}
            pagination={{
              current: currentPage,
              pageSize,
              total: data?.count ?? 0,
              showSizeChanger: true,
              pageSizeOptions: [10, 20, 50, 100],
              showTotal: (total) => `共 ${total} 条`,
              onChange: handlePageChange,
            }}
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

      {backupOpen && (
        <TagBackupModal open onClose={() => setBackupOpen(false)} onRestored={() => reload()} />
      )}
    </div>
  )
}
