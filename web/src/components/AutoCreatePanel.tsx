import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Alert,
  App as AntdApp,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  InputNumber,
  Progress,
  Select,
  Space,
  Tag,
  Typography,
} from 'antd'
import { PauseCircleOutlined, PlayCircleOutlined, ReloadOutlined } from '@ant-design/icons'
import { ApiError, request } from '../api/client'
import { describeError } from '../api/hooks'
import type { AccountSummary, AutoCreateStartRequest, AutoCreateStatus } from '../api/types'
import { formatDateTime } from '../utils/format'

const PHASE_META: Record<string, { text: string; color: string }> = {
  idle: { text: '未运行', color: 'default' },
  running: { text: '创建中', color: 'processing' },
  cooling: { text: '冷却中', color: 'warning' },
  completed: { text: '已完成', color: 'success' },
  stopped: { text: '已停止', color: 'default' },
}

const LEVEL_COLOR: Record<string, string> = {
  info: '#64748b',
  success: '#0f766e',
  warn: '#d97706',
  error: '#dc2626',
}

interface Props {
  open: boolean
  /** 当前页面选中的账号，作为默认勾选项 */
  accountId: string
  accounts: AccountSummary[]
  onClose: () => void
  /** 任务创建出新别名后回调，用于刷新账号与别名列表 */
  onProgress: () => void
}

/** 后台自动建满别名：可一次勾选多个账号并行创建，并展示逐账号进度 */
export default function AutoCreatePanel({ open, accountId, accounts, onClose, onProgress }: Props) {
  const { message } = AntdApp.useApp()
  const [form] = Form.useForm<AutoCreateStartRequest>()
  const [status, setStatus] = useState<AutoCreateStatus | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [version, setVersion] = useState(0)

  const refresh = useCallback(() => setVersion((v) => v + 1), [])

  // 打开面板时轮询状态；任务运行时持续跟随
  useEffect(() => {
    if (!open) return
    let cancelled = false
    const tick = () => {
      request<AutoCreateStatus>('/api/autocreate')
        .then((data) => {
          if (cancelled) return
          setStatus(data)
          setError('')
        })
        .catch((err) => {
          if (cancelled) return
          setError(describeError(err))
        })
    }
    const timer = window.setInterval(tick, 3000)
    tick()
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [open, version])

  // 回调用 ref 持有:父组件即便传入不稳定引用,也不会因为依赖变化反复触发刷新
  // (ref 只在 effect 中更新,渲染期间读写 ref 在并发渲染下不安全)
  const onProgressRef = useRef(onProgress)
  useEffect(() => {
    onProgressRef.current = onProgress
  }, [onProgress])

  // 只在"创建数增加"时通知外部刷新,且 effect 只依赖 created
  const lastCreatedRef = useRef(0)
  useEffect(() => {
    const created = status?.created ?? 0
    if (created > lastCreatedRef.current) {
      lastCreatedRef.current = created
      onProgressRef.current()
    }
  }, [status?.created])

  const accountOptions = useMemo(
    () => accounts.map((account) => ({ value: account.id, label: account.name })),
    [accounts],
  )

  async function handleStart() {
    let values: AutoCreateStartRequest
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    if (!values.account_ids?.length) {
      setError('请至少选择一个账号')
      return
    }
    setBusy(true)
    setError('')
    try {
      const next = await request<AutoCreateStatus>('/api/autocreate/start', {
        method: 'POST',
        body: { ...values, account_id: values.account_ids[0] },
      })
      setStatus(next)
      message.success(
        next.phase === 'completed' ? '所选账号均已达目标,无需继续创建' : `已启动 ${next.accounts.length} 个账号的创建任务`,
      )
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setBusy(false)
    }
  }

  async function handleStop() {
    setBusy(true)
    try {
      setStatus(await request<AutoCreateStatus>('/api/autocreate/stop', { method: 'POST' }))
      message.success('任务已停止')
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setBusy(false)
    }
  }

  const phase = PHASE_META[status?.phase ?? 'idle'] ?? { text: status?.phase ?? '未知', color: 'default' }
  const percent =
    status && status.target > 0 && status.accounts?.length
      ? Math.min(100, Math.round((status.total / (status.target * status.accounts.length)) * 100))
      : 0
  const tasks = status?.accounts ?? []

  return (
    <Drawer
      open={open}
      size={680}
      title="自动建满别名"
      onClose={onClose}
      extra={<Button size="small" icon={<ReloadOutlined />} onClick={refresh} aria-label="刷新状态" />}
    >
      <Alert
        type="warning"
        showIcon
        title="任务会在后台真实创建别名"
        description="可一次勾选多个账号并行创建：iCloud 的创建限流按账号计算，多账号并行才能线性提升总吞吐。每创建一个别名后会随机等待一段时间再继续（默认 20–40 秒，避免固定节拍）。达到目标总数或被 iCloud 判定为配额已满时该账号结束；命中速率限制会按设定冷却（默认 30 分钟）后继续。服务重启后任务不会自动恢复。"
        style={{ marginBottom: 16 }}
      />

      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}

      <Space style={{ marginBottom: 12 }} wrap>
        <Tag color={phase.color}>{phase.text}</Tag>
        {tasks.length > 0 && <Tag>{tasks.length} 个账号</Tag>}
        {status?.running && status.next_run_at && (
          <Typography.Text type="secondary">下次执行：{formatDateTime(status.next_run_at)}</Typography.Text>
        )}
        {status?.attempting && (
          <Typography.Text type="secondary">
            正在创建中（单次尝试含 iCloud 校验,可能持续数十秒）…
          </Typography.Text>
        )}
      </Space>

      <Progress
        percent={percent}
        status={status?.phase === 'completed' ? 'success' : status?.running ? 'active' : 'normal'}
        format={() => `${status?.total ?? 0} / ${(status?.target ?? 0) * Math.max(tasks.length, 1)}`}
        style={{ marginBottom: 12 }}
      />

      <Descriptions
        size="small"
        column={2}
        style={{ marginBottom: 16 }}
        items={[
          { key: 'created', label: '本次创建', children: status?.created ?? 0 },
          { key: 'failed', label: '失败次数', children: status?.failed ?? 0 },
          {
            key: 'interval',
            label: '创建后等待',
            children: `${status?.interval_seconds ?? 20}–${status?.interval_max_seconds ?? 40} 秒（随机）`,
          },
          { key: 'cooldown', label: '冷却', children: `${Math.round((status?.cooldown_seconds ?? 1800) / 60)} 分钟` },
          { key: 'parallel', label: '并发上限', children: status?.max_parallel ?? 10 },
          { key: 'last', label: '最近创建', children: status?.last_email || '—' },
        ]}
      />

      {tasks.length > 0 && (
        <div style={{ marginBottom: 16, border: '1px solid #e6e8eb', borderRadius: 8, padding: '4px 12px' }}>
          {tasks.map((task) => {
            const meta = PHASE_META[task.phase] ?? { text: task.phase, color: 'default' }
            const done = task.target > 0 ? Math.min(100, Math.round((task.total / task.target) * 100)) : 0
            return (
              <div key={task.account_id} style={{ padding: '10px 0', borderBottom: '1px solid #f1f5f9' }}>
                <Space wrap size={8}>
                  <Typography.Text strong>{task.name || task.account_id}</Typography.Text>
                  <Tag color={meta.color}>{meta.text}</Tag>
                  <Typography.Text type="secondary">
                    {task.total} / {task.target}
                    {task.remaining > 0 ? `（还需 ${task.remaining}）` : ''}
                  </Typography.Text>
                  {task.attempting && <Typography.Text type="secondary">创建中…</Typography.Text>}
                </Space>
                <Progress percent={done} size="small" showInfo={false} style={{ margin: '4px 0 0' }} />
                {task.capacity_error && (
                  <div className="mono" style={{ fontSize: 12, color: '#d97706' }}>
                    配额检查失败：{task.capacity_error}
                  </div>
                )}
                {task.last_email && (
                  <div className="mono" style={{ fontSize: 12, color: '#0f766e' }}>
                    最近创建：{task.last_email}
                  </div>
                )}
                {task.last_error && (
                  <div className="mono" style={{ fontSize: 12, color: '#dc2626' }}>
                    最近错误：{task.last_error}
                  </div>
                )}
                {task.reason && (
                  <div style={{ fontSize: 12, color: '#64748b' }}>{task.reason}</div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {status?.reason && (
        <Alert
          type={status.phase === 'completed' ? 'success' : 'info'}
          showIcon
          title={status.reason}
          style={{ marginBottom: 16 }}
        />
      )}

      {!status?.running && (
        <Form
          form={form}
          layout="vertical"
          initialValues={{
            account_ids: accountId ? [accountId] : [],
            target: 700,
            interval_seconds: 20,
            interval_max_seconds: 40,
            cooldown_seconds: 1800,
            label_prefix: 'auto',
            max_failures: 5,
            max_parallel: 10,
          }}
        >
          <Form.Item
            name="account_ids"
            label="账号（可多选，并行创建）"
            rules={[{ required: true, message: '请至少选择一个账号' }]}
          >
            <Select
              mode="multiple"
              options={accountOptions}
              placeholder="选择要建满的账号"
              maxTagCount={4}
            />
          </Form.Item>
          <Space wrap size={12}>
            <Form.Item name="target" label="每账号目标总数">
              <InputNumber min={1} max={5000} style={{ width: 140 }} />
            </Form.Item>
            <Form.Item name="interval_seconds" label="间隔最小（秒）">
              <InputNumber min={5} max={3600} style={{ width: 130 }} />
            </Form.Item>
            <Form.Item name="interval_max_seconds" label="间隔最大（秒）">
              <InputNumber min={5} max={3600} style={{ width: 130 }} />
            </Form.Item>
            <Form.Item name="cooldown_seconds" label="冷却（秒）">
              <InputNumber min={60} max={86400} style={{ width: 130 }} />
            </Form.Item>
            <Form.Item name="max_parallel" label="并发上限">
              <InputNumber min={1} max={20} style={{ width: 110 }} />
            </Form.Item>
          </Space>
          <Space wrap size={12}>
            <Form.Item name="label_prefix" label="标签前缀">
              <Input style={{ width: 130 }} maxLength={50} />
            </Form.Item>
            <Form.Item name="max_failures" label="连续失败上限">
              <InputNumber min={1} max={100} style={{ width: 130 }} />
            </Form.Item>
          </Space>
          <Space>
            <Button type="primary" icon={<PlayCircleOutlined />} loading={busy} onClick={() => void handleStart()}>
              启动
            </Button>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              每创建一个后随机等待 20–40 秒;目标按账号别名总数计算,默认 700 为社区实测经验上限
            </Typography.Text>
          </Space>
        </Form>
      )}

      {status?.running && (
        <Button danger icon={<PauseCircleOutlined />} loading={busy} onClick={() => void handleStop()}>
          停止任务
        </Button>
      )}

      <div style={{ marginTop: 20 }}>
        <Typography.Text strong>运行日志</Typography.Text>
        <div
          style={{
            marginTop: 8,
            maxHeight: 240,
            overflow: 'auto',
            border: '1px solid #e6e8eb',
            borderRadius: 8,
            padding: '8px 12px',
            background: '#f8fafc',
          }}
        >
          {(status?.logs ?? []).length === 0 ? (
            <Typography.Text type="secondary">暂无日志</Typography.Text>
          ) : (
            [...(status?.logs ?? [])].reverse().map((log, index) => (
              <div key={`${log.time}-${index}`} className="mono" style={{ fontSize: 12, lineHeight: 1.7 }}>
                <span style={{ color: '#94a3b8' }}>{formatDateTime(log.time)}</span>{' '}
                <span style={{ color: LEVEL_COLOR[log.level] ?? '#64748b' }}>{log.message}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </Drawer>
  )
}
