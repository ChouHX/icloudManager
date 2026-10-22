import { useEffect, useMemo, useState } from 'react'
import {
  Alert,
  App as AntdApp,
  Button,
  Card,
  Col,
  DatePicker,
  Divider,
  Form,
  Input,
  Modal,
  Popconfirm,
  Popover,
  Progress,
  Result,
  Row,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableProps } from 'antd'
import type { Dayjs } from 'dayjs'
import {
  BarChartOutlined,
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  ExportOutlined,
  PlusOutlined,
  ReloadOutlined,
  TagOutlined,
  TagsOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { ApiError, request } from '../api/client'
import { useDashboard, useTags } from '../api/hooks'
import type { DashboardData, TagBatchFilter, TagEntry, TagEntryInput } from '../api/types'
import { formatDateTime } from '../utils/format'
import {
  TagBackupModal,
  TagBatchModal,
  TagDeleteModal,
  TagExportModal,
  TagImportModal,
  TagManageModal,
} from '../components/TagModals'

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

/** Popover 内的迷你统计格 */
function StatBox({ label, value }: { label: string; value: number }) {
  return (
    <div
      style={{
        background: '#f8fafc',
        border: '1px solid #eef1f5',
        borderRadius: 8,
        padding: '6px 8px',
        textAlign: 'center',
      }}
    >
      <div style={{ fontSize: 16, fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}>
        {value}
      </div>
      <div style={{ fontSize: 11, color: '#64748b' }}>{label}</div>
    </div>
  )
}

/** 统计数据 Popover:汇总标签账号 + 网站/标签分布 */
function TagStatsPopover({
  loading,
  tags,
  onOpenDashboard,
}: {
  loading: boolean
  tags: DashboardData['tags'] | null
  onOpenDashboard: () => void
}) {
  if (loading && !tags) {
    return (
      <div style={{ width: 380, textAlign: 'center', padding: 16 }}>
        <Spin size="small" />
      </div>
    )
  }

  const bySite = tags?.by_site ?? []
  const byTag = tags?.by_tag ?? []
  const maxSite = bySite.reduce((max, item) => Math.max(max, item.count), 0)
  const maxTag = byTag.reduce((max, item) => Math.max(max, item.count), 0)

  return (
    <div style={{ width: 380 }}>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 8 }}>
        <StatBox label="记录总数" value={tags?.total ?? 0} />
        <StatBox label="已打标签" value={tags?.tagged ?? 0} />
        <StatBox label="未打标签" value={tags?.untagged ?? 0} />
        <StatBox label="标签种类" value={tags?.tag_kinds ?? 0} />
        <StatBox label="网站种类" value={tags?.site_kinds ?? 0} />
      </div>

      <Divider style={{ margin: '12px 0' }} />

      <Row gutter={16}>
        <Col span={12}>
          <Typography.Text strong style={{ fontSize: 13 }}>
            按网站
          </Typography.Text>
          {bySite.length === 0 ? (
            <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
              暂无数据
            </Typography.Text>
          ) : (
            bySite.slice(0, 5).map((item) => (
              <div key={item.site} style={{ marginTop: 6 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}>
                  <Typography.Text ellipsis style={{ maxWidth: 110, fontSize: 12 }}>
                    {item.site}
                  </Typography.Text>
                  <span style={{ fontVariantNumeric: 'tabular-nums' }}>{item.count}</span>
                </div>
                <Progress
                  percent={maxSite > 0 ? Math.round((item.count / maxSite) * 100) : 0}
                  showInfo={false}
                  strokeWidth={4}
                />
              </div>
            ))
          )}
        </Col>
        <Col span={12}>
          <Typography.Text strong style={{ fontSize: 13 }}>
            按标签
          </Typography.Text>
          {byTag.length === 0 ? (
            <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12 }}>
              暂无数据
            </Typography.Text>
          ) : (
            byTag.slice(0, 5).map((item) => (
              <div key={item.tag} style={{ marginTop: 6 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12 }}>
                  <Typography.Text ellipsis style={{ maxWidth: 110, fontSize: 12 }}>
                    {item.tag}
                  </Typography.Text>
                  <span style={{ fontVariantNumeric: 'tabular-nums' }}>{item.count}</span>
                </div>
                <Progress
                  percent={maxTag > 0 ? Math.round((item.count / maxTag) * 100) : 0}
                  showInfo={false}
                  strokeWidth={4}
                />
              </div>
            ))
          )}
        </Col>
      </Row>

      <Divider style={{ margin: '12px 0' }} />
      <Button type="link" size="small" style={{ padding: 0 }} onClick={onOpenDashboard}>
        查看完整数据看板 →
      </Button>
    </div>
  )
}

export default function TagsPage() {
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [searchInput, setSearchInput] = useState('')
  const [search, setSearch] = useState('')
  const [selectedTags, setSelectedTags] = useState<string[]>([])
  const [site, setSite] = useState('')
  const [dateRange, setDateRange] = useState<[Dayjs | null, Dayjs | null] | null>(null)
  const [selectedIds, setSelectedIds] = useState<string[]>([])
  const [exportOpen, setExportOpen] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)

  // 输入防抖,避免每敲一个字符都请求服务端
  useEffect(() => {
    const trimmed = searchInput.trim()
    if (trimmed === search) return
    const timer = setTimeout(() => {
      setSearch(trimmed)
      setPage(1)
      setSelectedIds([])
    }, 300)
    return () => clearTimeout(timer)
  }, [searchInput, search])

  const query = useMemo(() => {
    const params = new URLSearchParams()
    params.set('page', String(page))
    params.set('page_size', String(pageSize))
    if (search) params.set('search', search)
    if (selectedTags.length > 0) params.set('tags', selectedTags.join(','))
    if (site) params.set('site', site)
    if (dateRange?.[0]) params.set('created_from', dateRange[0].startOf('day').toISOString())
    if (dateRange?.[1]) params.set('created_to', dateRange[1].endOf('day').toISOString())
    return `?${params.toString()}`
  }, [page, pageSize, search, selectedTags, site, dateRange])

  const { data, loading, error, reload } = useTags(query)
  const { data: dashboard, loading: dashboardLoading, reload: reloadDashboard } = useDashboard()
  const { message } = AntdApp.useApp()
  const navigate = useNavigate()

  /** 同时刷新列表与统计数据 */
  function refreshAll() {
    reload()
    reloadDashboard()
  }

  const [formOpen, setFormOpen] = useState(false)
  const [importOpen, setImportOpen] = useState(false)
  const [manageOpen, setManageOpen] = useState(false)
  const [backupOpen, setBackupOpen] = useState(false)
  const [batchOpen, setBatchOpen] = useState(false)
  const [editing, setEditing] = useState<TagEntry | null>(null)

  const entries = useMemo(() => data?.entries ?? [], [data])
  const tagOptions = useMemo(() => data?.tags ?? [], [data])
  const siteOptions = useMemo(() => data?.sites ?? [], [data])
  const stats = data?.stats ?? { total: 0, tags: 0, sites: 0, untagged: 0 }
  const hasFilters = search !== '' || selectedTags.length > 0 || site !== '' || dateRange !== null
  const batchFilter = useMemo<TagBatchFilter>(
    () => ({
      search: search || undefined,
      tags: selectedTags.length > 0 ? selectedTags : undefined,
      site: site || undefined,
      created_from: dateRange?.[0] ? dateRange[0].startOf('day').toISOString() : undefined,
      created_to: dateRange?.[1] ? dateRange[1].endOf('day').toISOString() : undefined,
    }),
    [search, selectedTags, site, dateRange],
  )
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
      refreshAll()
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
        <div>
          <h2>标签账号</h2>
          <p>记录邮箱、网站密码与项目标签，用于区分每个项目使用的账号（与 iCloud 账号无关）</p>
        </div>
        <Space wrap>
          <Popover
            trigger="click"
            placement="bottomLeft"
            content={
              <TagStatsPopover
                loading={dashboardLoading}
                tags={dashboard?.tags ?? null}
                onOpenDashboard={() => navigate('/dashboard')}
              />
            }
          >
            <Button icon={<BarChartOutlined />} disabled={stats.total === 0}>
              统计数据
            </Button>
          </Popover>
          <Button icon={<UploadOutlined />} onClick={() => setImportOpen(true)}>
            批量导入
          </Button>
          <Button
            icon={<TagOutlined />}
            onClick={() => setBatchOpen(true)}
            disabled={(data?.count ?? 0) === 0 && selectedIds.length === 0}
          >
            批量打标签
          </Button>
          <Button
            icon={<ExportOutlined />}
            onClick={() => setExportOpen(true)}
            disabled={stats.total === 0}
          >
            批量导出
          </Button>
          <Button
            danger
            icon={<DeleteOutlined />}
            onClick={() => setDeleteOpen(true)}
            disabled={stats.total === 0}
          >
            批量删除
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
              setSelectedIds([])
            }}
            options={tagOptions.map((tag) => ({ value: tag, label: tag }))}
            maxTagCount="responsive"
          />
          <Select
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder="按网站筛选"
            style={{ minWidth: 150 }}
            value={site || undefined}
            onChange={(value) => {
              setSite(value ?? '')
              setPage(1)
              setSelectedIds([])
            }}
            options={siteOptions.map((item) => ({ value: item, label: item }))}
          />
          <DatePicker.RangePicker
            value={dateRange}
            onChange={(dates) => {
              setDateRange(dates)
              setPage(1)
              setSelectedIds([])
            }}
            placeholder={['创建起始', '创建结束']}
            title="按创建时间筛选（批量导入的记录即导入时间）"
          />
          <Button icon={<ReloadOutlined />} onClick={refreshAll} aria-label="刷新列表" />
        </div>

        {error && (
          <Alert
            type="error"
            showIcon
            message={error}
            style={{ marginBottom: 16 }}
            action={
              <Button size="small" onClick={refreshAll}>
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
            rowSelection={{
              selectedRowKeys: selectedIds,
              onChange: (keys) => setSelectedIds(keys.map(String)),
              preserveSelectedRowKeys: true,
            }}
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
          refreshAll()
        }}
      />

      {batchOpen && (
        <TagBatchModal
          open
          filteredCount={data?.count ?? 0}
          selectedIds={selectedIds}
          filter={batchFilter}
          tagOptions={tagOptions}
          onClose={() => setBatchOpen(false)}
          onDone={() => {
            refreshAll()
            setSelectedIds([])
          }}
        />
      )}

      {exportOpen && (
        <TagExportModal
          open
          filteredCount={data?.count ?? 0}
          totalCount={stats.total}
          selectedIds={selectedIds}
          filter={batchFilter}
          filterActive={hasFilters}
          onClose={() => setExportOpen(false)}
        />
      )}

      {deleteOpen && (
        <TagDeleteModal
          open
          filteredCount={data?.count ?? 0}
          totalCount={stats.total}
          selectedIds={selectedIds}
          filter={batchFilter}
          filterActive={hasFilters}
          onClose={() => setDeleteOpen(false)}
          onDone={() => {
            refreshAll()
            setSelectedIds([])
          }}
        />
      )}

      {importOpen && (
        <TagImportModal
          open
          tagOptions={tagOptions}
          onClose={() => setImportOpen(false)}
          onImported={() => refreshAll()}
        />
      )}

      {manageOpen && (
        <TagManageModal
          open
          tags={tagOptions}
          counts={tagCounts}
          onClose={() => setManageOpen(false)}
          onChanged={() => refreshAll()}
        />
      )}

      {backupOpen && (
        <TagBackupModal open onClose={() => setBackupOpen(false)} onRestored={() => refreshAll()} />
      )}
    </div>
  )
}
