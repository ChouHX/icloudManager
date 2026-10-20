import { useEffect, useRef, useState } from 'react'
import type { ChangeEvent } from 'react'
import {
  Alert,
  App as AntdApp,
  Button,
  Divider,
  Form,
  Input,
  Modal,
  Popconfirm,
  Radio,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { InputRef, TableProps } from 'antd'
import {
  DeleteOutlined,
  DownloadOutlined,
  EditOutlined,
  InboxOutlined,
  PlusOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import type {
  TagBackup,
  TagBatchAction,
  TagBatchFilter,
  TagBatchInput,
  TagBatchResult,
  TagEntry,
  TagImportInput,
  TagImportResult,
  TagRestoreInput,
  TagRestoreResult,
} from '../api/types'

const IMPORT_FORMAT_HELP =
  '每行一条记录，支持「邮箱----密码----网站」，也支持制表符、竖线、逗号、英文冒号或空格分隔；' +
  '只有邮箱也可以。以 # 开头的行会被忽略。'

/** 批量导入标签凭据，并给所有导入的记录统一打上选定标签 */
export function TagImportModal({
  open,
  tagOptions,
  onClose,
  onImported,
}: {
  open: boolean
  tagOptions: string[]
  onClose: () => void
  onImported: () => void
}) {
  const [form] = Form.useForm()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [result, setResult] = useState<TagImportResult | null>(null)

  async function handleImport() {
    let values: { text?: string; tags?: string[]; site?: string; mode?: 'skip' | 'update' }
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    const body: TagImportInput = {
      text: values.text ?? '',
      tags: values.tags ?? [],
      site: (values.site ?? '').trim(),
      mode: values.mode ?? 'skip',
    }
    setSubmitting(true)
    setError('')
    try {
      const res = await request<TagImportResult>('/api/tags/import', { method: 'POST', body })
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
      title="批量导入账号"
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
        <Space direction="vertical" style={{ width: '100%' }} size={12}>
          <Alert
            type={result.invalid > 0 ? 'warning' : 'success'}
            showIcon
            message="导入完成"
            description={
              <Space size={16} wrap>
                <span>新增 <strong>{result.imported}</strong></span>
                <span>更新 <strong>{result.updated}</strong></span>
                <span>跳过 <strong>{result.skipped}</strong></span>
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
        <Form form={form} layout="vertical" requiredMark="optional" initialValues={{ mode: 'skip' }}>
          <Form.Item
            name="text"
            label="账号列表"
            extra={IMPORT_FORMAT_HELP}
            rules={[{ required: true, message: '请粘贴要导入的账号' }]}
          >
            <Input.TextArea
              rows={10}
              spellCheck={false}
              placeholder={'user1@example.com----pass1\nuser2@example.com----pass2----GitHub\nuser3@example.com'}
            />
          </Form.Item>
          <Form.Item
            name="tags"
            label="统一标签"
            extra="本次导入的所有账号都会打上这些项目标签，可输入新标签后回车"
          >
            <Select
              mode="tags"
              placeholder="例如：项目A、测试"
              options={tagOptions.map((tag) => ({ value: tag, label: tag }))}
              tokenSeparators={[',', '，', ' ']}
              maxTagCount="responsive"
            />
          </Form.Item>
          <Form.Item name="site" label="默认网站" extra="行内未指定网站时使用，例如 GitHub">
            <Input placeholder="可留空" maxLength={100} />
          </Form.Item>
          <Form.Item name="mode" label="已存在的记录（邮箱 + 网站相同）">
            <Radio.Group
              options={[
                { value: 'skip', label: '跳过，不修改已有记录' },
                { value: 'update', label: '覆盖更新密码并合并标签' },
              ]}
            />
          </Form.Item>
        </Form>
      )}
    </Modal>
  )
}

interface TagUsage {
  tag: string
  count: number
}

/** 标签管理：全局重命名 / 删除标签 */
export function TagManageModal({
  open,
  tags,
  counts,
  onClose,
  onChanged,
}: {
  open: boolean
  tags: string[]
  counts: Record<string, number>
  onClose: () => void
  onChanged: () => void
}) {
  const { message } = AntdApp.useApp()
  const [editing, setEditing] = useState('')
  const [value, setValue] = useState('')
  const [pending, setPending] = useState('')
  const inputRef = useRef<InputRef>(null)

  useEffect(() => {
    if (editing) inputRef.current?.focus()
  }, [editing])

  const rows: TagUsage[] = tags.map((tag) => ({ tag, count: counts[tag.toLowerCase()] ?? 0 }))

  async function rename(from: string) {
    const to = value.trim()
    if (!to || to === from) {
      setEditing('')
      return
    }
    setPending(from)
    try {
      const res = await request<{ changed: number }>('/api/tag-labels/rename', {
        method: 'PUT',
        body: { from, to },
      })
      message.success(`已将「${from}」重命名为「${to}」，影响 ${res.changed} 条记录`)
      setEditing('')
      onChanged()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '重命名失败，请重试')
    } finally {
      setPending('')
    }
  }

  async function remove(tag: string) {
    setPending(tag)
    try {
      const res = await request<{ changed: number }>(
        `/api/tag-labels/${encodeURIComponent(tag)}`,
        { method: 'DELETE' },
      )
      message.success(`已删除标签「${tag}」，影响 ${res.changed} 条记录`)
      onChanged()
    } catch (err) {
      message.error(err instanceof ApiError ? err.message : '删除失败，请重试')
    } finally {
      setPending('')
    }
  }

  const columns: TableProps<TagUsage>['columns'] = [
    {
      title: '标签',
      dataIndex: 'tag',
      render: (tag: string) =>
        editing === tag ? (
          <Input
            ref={inputRef}
            size="small"
            value={value}
            maxLength={32}
            onChange={(e) => setValue(e.target.value)}
            onPressEnter={() => void rename(tag)}
            aria-label={`重命名 ${tag}`}
          />
        ) : (
          <Tag color="blue">{tag}</Tag>
        ),
    },
    {
      title: '使用记录数',
      dataIndex: 'count',
      width: 120,
      render: (count: number) => <Typography.Text>{count}</Typography.Text>,
    },
    {
      title: '操作',
      key: 'actions',
      width: 160,
      render: (_, row) =>
        editing === row.tag ? (
          <Space size={4}>
            <Button
              type="link"
              size="small"
              loading={pending === row.tag}
              onClick={() => void rename(row.tag)}
            >
              保存
            </Button>
            <Button type="link" size="small" onClick={() => setEditing('')}>
              取消
            </Button>
          </Space>
        ) : (
          <Space size={4}>
            <Button
              type="link"
              size="small"
              icon={<EditOutlined />}
              onClick={() => {
                setEditing(row.tag)
                setValue(row.tag)
              }}
            >
              重命名
            </Button>
            <Popconfirm
              title={`删除标签「${row.tag}」？`}
              description="会从所有记录中移除该标签，不会删除记录本身。"
              okText="删除"
              okButtonProps={{ danger: true }}
              cancelText="取消"
              onConfirm={() => void remove(row.tag)}
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
    <Modal
      open={open}
      title="标签管理"
      width={640}
      onCancel={onClose}
      destroyOnHidden
      mask={{ closable: false }}
      footer={
        <Button type="primary" onClick={onClose}>
          完成
        </Button>
      }
    >
      {rows.length === 0 ? (
        <Alert type="info" showIcon message="还没有任何标签，导入或编辑记录时即可添加。" />
      ) : (
        <Table<TagUsage>
          rowKey="tag"
          columns={columns}
          dataSource={rows}
          pagination={false}
          size="small"
          scroll={{ y: 360 }}
        />
      )}
      <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
        <PlusOutlined /> 在「添加记录」或「批量导入」的标签输入框中回车即可创建新标签。
      </Typography.Paragraph>
    </Modal>
  )
}

/** 从备份文档中提取记录,兼容备份对象、API 响应包裹与纯数组三种形态 */
function extractBackupEntries(parsed: unknown): TagEntry[] | null {
  if (Array.isArray(parsed)) {
    return parsed as TagEntry[]
  }
  if (parsed && typeof parsed === 'object') {
    const obj = parsed as Record<string, unknown>
    if (Array.isArray(obj.entries)) {
      return obj.entries as TagEntry[]
    }
    const data = obj.data
    if (data && typeof data === 'object' && Array.isArray((data as Record<string, unknown>).entries)) {
      return (data as Record<string, unknown>).entries as TagEntry[]
    }
  }
  return null
}

function downloadJSON(payload: unknown, filename: string): void {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

function backupFileName(exportedAt: string): string {
  const stamp = (exportedAt || new Date().toISOString())
    .replace(/[:.]/g, '-')
    .replace(/\+.*$/, '')
  return `tags-backup-${stamp}.json`
}

/** 备份导出 / 导入,用于跨实例迁移数据 */
export function TagBackupModal({
  open,
  onClose,
  onRestored,
}: {
  open: boolean
  onClose: () => void
  onRestored: () => void
}) {
  const [exporting, setExporting] = useState(false)
  const [importing, setImporting] = useState(false)
  const [mode, setMode] = useState<'merge' | 'replace'>('merge')
  const [text, setText] = useState('')
  const [fileName, setFileName] = useState('')
  const [error, setError] = useState('')
  const [result, setResult] = useState<TagRestoreResult | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  async function handleExport() {
    setExporting(true)
    setError('')
    try {
      const backup = await request<TagBackup>('/api/tags/export')
      downloadJSON(backup, backupFileName(backup.exported_at))
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '导出失败，请重试')
    } finally {
      setExporting(false)
    }
  }

  async function handleImport() {
    setError('')
    setResult(null)
    let parsed: unknown
    try {
      parsed = JSON.parse(text)
    } catch {
      setError('内容不是有效的 JSON，请选择导出的备份文件或粘贴其内容')
      return
    }
    const entries = extractBackupEntries(parsed)
    if (!entries || entries.length === 0) {
      setError('未在备份中找到任何记录')
      return
    }
    const body: TagRestoreInput = { entries, mode }
    setImporting(true)
    try {
      const res = await request<TagRestoreResult>('/api/tags/restore', { method: 'POST', body })
      setResult(res)
      onRestored()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '导入失败，请重试')
    } finally {
      setImporting(false)
    }
  }

  function handlePickFile(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    if (!file) return
    setFileName(file.name)
    const reader = new FileReader()
    reader.onload = () => {
      setText(typeof reader.result === 'string' ? reader.result : '')
      setError('')
      setResult(null)
    }
    reader.readAsText(file)
    event.target.value = ''
  }

  return (
    <Modal
      open={open}
      title="备份与迁移"
      width={640}
      onCancel={onClose}
      destroyOnHidden
      mask={{ closable: false }}
      footer={
        <Button type="primary" onClick={onClose}>
          完成
        </Button>
      }
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}

      <Typography.Title level={5} style={{ marginTop: 0 }}>
        导出备份
      </Typography.Title>
      <Typography.Paragraph type="secondary">
        导出全部标签账号为 JSON 文件，包含邮箱、密码、网站与项目标签，可用于在其他实例恢复。
      </Typography.Paragraph>
      <Button icon={<DownloadOutlined />} loading={exporting} onClick={() => void handleExport()}>
        导出备份文件
      </Button>

      <Divider />

      <Typography.Title level={5}>导入备份</Typography.Title>
      <Typography.Paragraph type="secondary">
        选择或粘贴之前导出的 JSON 文件，将记录恢复到当前实例。
      </Typography.Paragraph>

      {result ? (
        <Alert
          type={result.invalid > 0 ? 'warning' : 'success'}
          showIcon
          message="导入完成"
          description={
            <Space direction="vertical" size={4}>
              <Space size={16} wrap>
                <span>新增 <strong>{result.imported}</strong></span>
                <span>跳过 <strong>{result.skipped}</strong></span>
                <span>无效 <strong>{result.invalid}</strong></span>
                <span>当前共 <strong>{result.total}</strong> 条</span>
              </Space>
              {result.errors.length > 0 && (
                <ul style={{ margin: 0, paddingLeft: 18 }}>
                  {result.errors.map((item) => (
                    <li key={item}>
                      <Typography.Text type="secondary">{item}</Typography.Text>
                    </li>
                  ))}
                </ul>
              )}
            </Space>
          }
        />
      ) : (
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
          <Space wrap>
            <Button icon={<InboxOutlined />} onClick={() => fileInputRef.current?.click()}>
              选择备份文件
            </Button>
            <input
              ref={fileInputRef}
              type="file"
              accept="application/json,.json"
              style={{ display: 'none' }}
              onChange={handlePickFile}
              aria-label="选择备份文件"
            />
            {fileName && <Typography.Text type="secondary">{fileName}</Typography.Text>}
          </Space>
          <Input.TextArea
            rows={6}
            spellCheck={false}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="也可以直接粘贴备份 JSON 内容"
            aria-label="备份 JSON 内容"
          />
          <Radio.Group
            value={mode}
            onChange={(e) => setMode(e.target.value as 'merge' | 'replace')}
            options={[
              { value: 'merge', label: '合并：保留现有记录，跳过重复' },
              { value: 'replace', label: '覆盖：清空后仅保留备份内容' },
            ]}
          />
          {mode === 'replace' && (
            <Alert type="warning" showIcon message="覆盖导入会先清空当前所有标签账号，请谨慎操作。" />
          )}
          <Button
            type="primary"
            icon={<UploadOutlined />}
            loading={importing}
            disabled={!text.trim()}
            onClick={() => void handleImport()}
          >
            导入备份
          </Button>
        </Space>
      )}
    </Modal>
  )
}

const BATCH_ACTION_LABEL: Record<TagBatchAction, string> = {
  add: '追加标签',
  remove: '移除标签',
  replace: '覆盖标签',
}

const BATCH_ACTION_HELP: Record<TagBatchAction, string> = {
  add: '保留记录原有的标签，只追加这里填写的标签（已存在则自动跳过）。',
  remove: '只删除这里填写的标签，记录的其他标签保持不变。',
  replace: '先清空这些记录的全部标签，再写入这里填写的标签。',
}

/** 按筛选结果或勾选记录批量追加/移除/覆盖标签 */
export function TagBatchModal({
  open,
  filteredCount,
  selectedIds,
  filter,
  tagOptions,
  onClose,
  onDone,
}: {
  open: boolean
  filteredCount: number
  selectedIds: string[]
  filter: TagBatchFilter
  tagOptions: string[]
  onClose: () => void
  onDone: () => void
}) {
  const { message } = AntdApp.useApp()
  const [scope, setScope] = useState<'filter' | 'ids'>(selectedIds.length > 0 ? 'ids' : 'filter')
  const [action, setAction] = useState<TagBatchAction>('add')
  const [tags, setTags] = useState<string[]>([])
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const targetCount = scope === 'ids' ? selectedIds.length : filteredCount
  const hasFilter = Boolean(
    filter.search ||
      filter.site ||
      filter.created_from ||
      filter.created_to ||
      (filter.tags && filter.tags.length > 0),
  )
  const missingFilter = scope === 'filter' && !hasFilter

  async function handleOk() {
    if (missingFilter) {
      setError('请先在列表上方设置筛选条件，或改为勾选具体记录')
      return
    }
    if (tags.length === 0) {
      setError('请至少填写一个标签')
      return
    }
    if (targetCount === 0) {
      setError('当前范围没有可修改的记录，请先调整筛选条件')
      return
    }
    const body: TagBatchInput = {
      action,
      tags,
      ...(scope === 'ids' ? { ids: selectedIds } : { filter }),
    }
    setSubmitting(true)
    setError('')
    try {
      const result = await request<TagBatchResult>('/api/tags/batch', { method: 'POST', body })
      message.success(`已为 ${result.affected} 条记录更新标签`)
      onDone()
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
      title="批量打标签"
      width={560}
      onCancel={onClose}
      onOk={() => void handleOk()}
      okText="执行"
      confirmLoading={submitting}
      okButtonProps={{ disabled: missingFilter }}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Space orientation="vertical" size={18} style={{ width: '100%' }}>
        <div>
          <Typography.Text strong>作用范围</Typography.Text>
          <Radio.Group
            value={scope}
            onChange={(e) => setScope(e.target.value as 'filter' | 'ids')}
            style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }}
          >
            <Radio value="filter">当前筛选结果（{filteredCount} 条）</Radio>
            <Radio value="ids" disabled={selectedIds.length === 0}>
              已勾选的记录（{selectedIds.length} 条）
            </Radio>
          </Radio.Group>
          {scope === 'filter' && filteredCount === 0 && (
            <Typography.Text type="secondary">
              当前筛选没有记录，请先调整上方的搜索、标签、网站或时间条件。
            </Typography.Text>
          )}
          {missingFilter && filteredCount > 0 && (
            <Alert
              type="warning"
              showIcon
              title="当前没有筛选条件"
              description="为避免误改全部记录，请先关闭弹窗并设置网站、时间、标签或搜索条件；也可以直接勾选具体记录。"
              style={{ marginTop: 8 }}
            />
          )}
        </div>

        <div>
          <Typography.Text strong>操作</Typography.Text>
          <Radio.Group
            value={action}
            onChange={(e) => setAction(e.target.value as TagBatchAction)}
            style={{ display: 'flex', flexDirection: 'column', gap: 8, marginTop: 8 }}
          >
            <Radio value="add">{BATCH_ACTION_LABEL.add}</Radio>
            <Radio value="remove">{BATCH_ACTION_LABEL.remove}</Radio>
            <Radio value="replace">{BATCH_ACTION_LABEL.replace}</Radio>
          </Radio.Group>
        </div>

        <div>
          <Typography.Text strong>标签</Typography.Text>
          <Select
            mode="tags"
            style={{ width: '100%', marginTop: 8 }}
            value={tags}
            onChange={setTags}
            placeholder="输入标签后回车，可填写多个"
            options={tagOptions.map((tag) => ({ value: tag, label: tag }))}
            tokenSeparators={[',', '，', ' ']}
            maxTagCount="responsive"
          />
          <Typography.Text type="secondary" style={{ display: 'block', marginTop: 6 }}>
            {BATCH_ACTION_HELP[action]}
          </Typography.Text>
        </div>

        <Alert
          type={action === 'replace' ? 'warning' : 'info'}
          showIcon
          title={`将对 ${targetCount} 条记录执行「${BATCH_ACTION_LABEL[action]}」`}
        />
      </Space>
    </Modal>
  )
}
