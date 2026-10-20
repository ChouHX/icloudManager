import { useEffect, useRef, useState } from 'react'
import {
  Alert,
  App as AntdApp,
  Button,
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
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import type { TagImportInput, TagImportResult } from '../api/types'

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
