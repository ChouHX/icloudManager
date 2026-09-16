import { useState } from 'react'
import { Alert, App as AntdApp, Form, Input, InputNumber, Modal, Select } from 'antd'
import { ApiError, request } from '../api/client'
import type { AccountSummary, MailboxSummary } from '../api/types'

type FormValues = Record<string, string | number | undefined>

interface FieldSpec {
  name: string
  label: string
  type?: 'text' | 'password' | 'textarea' | 'number'
  required?: boolean
  placeholder?: string
  help?: string
  disabled?: boolean
  initialValue?: string | number
  maxLength?: number
}

/** 通用凭据弹窗：一份配置驱动 Cookie / App 密码 / 代理等表单 */
export function CredentialModal({
  open,
  title,
  description,
  accountId,
  endpoint,
  method,
  fields,
  okText = '保存',
  buildBody,
  onClose,
  onSaved,
}: {
  open: boolean
  title: string
  description?: string
  accountId: string
  endpoint: string
  method: 'POST' | 'PUT' | 'PATCH'
  fields: FieldSpec[]
  okText?: string
  buildBody?: (values: FormValues) => unknown
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<FormValues>()
  const { message } = AntdApp.useApp()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function handleOk() {
    let values: FormValues
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSubmitting(true)
    setError('')
    try {
      const body = buildBody ? buildBody(values) : values
      await request(endpoint.replace(':id', encodeURIComponent(accountId)), { method, body })
      onSaved()
      onClose()
      form.resetFields()
      message.success(`${title}已提交`)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title={title}
      onCancel={() => {
        setError('')
        onClose()
      }}
      onOk={() => void handleOk()}
      okText={okText}
      confirmLoading={submitting}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {description && <Alert type="info" showIcon title={description} style={{ marginBottom: 16 }} />}
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Form form={form} layout="vertical" requiredMark="optional" preserve={false}>
        {fields.map((field) => (
          <Form.Item
            key={field.name}
            name={field.name}
            label={field.label}
            initialValue={field.initialValue}
            extra={field.help}
            rules={field.required ? [{ required: true, message: `请填写${field.label}` }] : undefined}
          >
            {field.type === 'textarea' ? (
              <Input.TextArea rows={4} spellCheck={false} placeholder={field.placeholder} />
            ) : field.type === 'password' ? (
              <Input.Password autoComplete="off" placeholder={field.placeholder} />
            ) : field.type === 'number' ? (
              <InputNumber min={1} max={65535} style={{ width: '100%' }} />
            ) : (
              <Input
                maxLength={field.maxLength}
                disabled={field.disabled}
                placeholder={field.placeholder}
              />
            )}
          </Form.Item>
        ))}
      </Form>
    </Modal>
  )
}

/** 新增 / 编辑账号基本信息 */
export function AccountFormModal({
  open,
  editing,
  onClose,
  onSaved,
}: {
  open: boolean
  editing: AccountSummary | null
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // 弹窗每次打开都会重新挂载(destroyOnHidden + preserve={false}),
  // 因此这里的初始值对应当前编辑对象即可,无需在 effect 里同步表单。
  const initialValues = editing
    ? { name: editing.name, icloud_email: editing.icloud_email, host: editing.host }
    : { name: '', icloud_email: '', host: 'icloud.com', cookies: '', proxy: '' }

  async function handleOk() {
    let values: Record<string, string>
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSubmitting(true)
    setError('')
    try {
      if (editing) {
        await request(`/api/accounts/${encodeURIComponent(editing.id)}`, {
          method: 'PATCH',
          body: { name: values.name, host: values.host },
        })
      } else {
        await request('/api/accounts', {
          method: 'POST',
          body: {
            name: values.name,
            icloud_email: values.icloud_email,
            host: values.host,
            cookies: values.cookies ?? '',
            proxy: values.proxy ?? '',
          },
        })
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
      title={editing ? '编辑账号' : '添加账号'}
      onCancel={() => {
        setError('')
        onClose()
      }}
      onOk={() => void handleOk()}
      confirmLoading={submitting}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Form form={form} initialValues={initialValues} preserve={false} layout="vertical" requiredMark="optional">
        <Form.Item name="name" label="名称" rules={[{ required: true, message: '请填写账号名称' }, { max: 64, message: '最长 64 字符' }]}>
          <Input placeholder="例如：主号" maxLength={64} />
        </Form.Item>
        <Form.Item
          name="icloud_email"
          label="iCloud 邮箱"
          rules={[
            { required: true, message: '请填写 iCloud 邮箱' },
            { type: 'email', message: '邮箱格式不正确' },
          ]}
        >
          <Input placeholder="owner@icloud.com" disabled={Boolean(editing)} />
        </Form.Item>
        <Form.Item name="host" label="区域">
          <Select
            options={[
              { value: 'icloud.com', label: '全球区 (icloud.com)' },
              { value: 'icloud.com.cn', label: '中国区 (icloud.com.cn)' },
            ]}
            disabled={Boolean(editing)}
          />
        </Form.Item>
        {!editing && (
          <>
            <Form.Item
              name="cookies"
              label="Cookie（可选）"
              extra="支持 Cookie 头字符串或 JSON 文本；留空则账号处于待配置状态"
            >
              <Input.TextArea rows={3} spellCheck={false} placeholder="a=1; b=2" />
            </Form.Item>
            <Form.Item name="proxy" label="代理（可选）" extra="http / https / socks5 URL">
              <Input placeholder="http://user:pass@host:port" />
            </Form.Item>
          </>
        )}
      </Form>
    </Modal>
  )
}

/** iCloud 密码登录：支持 OTP 两阶段 */
export function ICloudLoginModal({
  open,
  accountId,
  onClose,
  onSaved,
}: {
  open: boolean
  accountId: string
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<{ password: string; otp_code?: string }>()
  const [otpRequired, setOtpRequired] = useState(false)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function handleOk() {
    let values: { password: string; otp_code?: string }
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSubmitting(true)
    setError('')
    try {
      await request(`/api/accounts/${encodeURIComponent(accountId)}/login`, {
        method: 'POST',
        body: { password: values.password, otp_code: values.otp_code ?? '' },
      })
      onSaved()
      onClose()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'OTP_REQUIRED') {
        setOtpRequired(true)
      } else {
        setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title="iCloud 密码登录"
      onCancel={() => {
        setError('')
        onClose()
      }}
      onOk={() => void handleOk()}
      okText={otpRequired ? '验证' : '登录'}
      confirmLoading={submitting}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {otpRequired && (
        <Alert
          type="warning"
          showIcon
          message="该账号启用了双重认证，请输入收到的 6 位验证码"
          style={{ marginBottom: 16 }}
        />
      )}
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Form form={form} layout="vertical" requiredMark="optional">
        <Form.Item name="password" label="iCloud 密码" rules={[{ required: true, message: '请填写密码' }]}>
          <Input.Password autoComplete="current-password" />
        </Form.Item>
        {otpRequired && (
          <Form.Item
            name="otp_code"
            label="验证码"
            rules={[{ required: true, message: '请填写验证码' }, { len: 6, message: '验证码为 6 位数字' }]}
          >
            <Input inputMode="numeric" maxLength={6} autoComplete="one-time-code" />
          </Form.Item>
        )}
      </Form>
    </Modal>
  )
}

const MAILBOX_PRESETS: Record<string, [string, number]> = {
  qq: ['imap.qq.com', 993],
  gmail: ['imap.gmail.com', 993],
  outlook: ['outlook.office365.com', 993],
  custom: ['', 993],
}

/** 接入外部收件邮箱（IMAP） */
export function MailboxModal({
  open,
  accountId,
  current,
  onClose,
  onSaved,
}: {
  open: boolean
  accountId: string
  current?: MailboxSummary
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const initialValues = {
    provider: current?.provider ?? 'qq',
    email: current?.email ?? '',
    imap_host: current?.imap_host ?? MAILBOX_PRESETS.qq[0],
    imap_port: current?.imap_port ?? MAILBOX_PRESETS.qq[1],
    authorization_code: '',
  }

  function handleProviderChange(value: string) {
    const preset = MAILBOX_PRESETS[value]
    if (preset) form.setFieldsValue({ imap_host: preset[0], imap_port: preset[1] })
  }

  async function handleOk() {
    let values: Record<string, string | number>
    try {
      values = await form.validateFields()
    } catch {
      return
    }
    setSubmitting(true)
    setError('')
    try {
      await request(`/api/accounts/${encodeURIComponent(accountId)}/mailbox`, {
        method: 'PUT',
        body: {
          provider: values.provider,
          email: values.email,
          imap_host: values.imap_host,
          imap_port: Number(values.imap_port),
          authorization_code: values.authorization_code,
        },
      })
      onSaved()
      onClose()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '收件邮箱接入失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal
      open={open}
      title="接入收件邮箱"
      onCancel={() => {
        setError('')
        onClose()
      }}
      onOk={() => void handleOk()}
      okText="验证并接入"
      confirmLoading={submitting}
      destroyOnHidden
      mask={{ closable: false }}
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
      <Form form={form} initialValues={initialValues} preserve={false} layout="vertical" requiredMark="optional">
        <Form.Item name="provider" label="邮箱服务商">
          <Select
            onChange={handleProviderChange}
            options={[
              { value: 'qq', label: 'QQ 邮箱' },
              { value: 'gmail', label: 'Gmail' },
              { value: 'outlook', label: 'Outlook' },
              { value: 'custom', label: '其他' },
            ]}
          />
        </Form.Item>
        <Form.Item name="email" label="收件邮箱" rules={[{ required: true, message: '请填写收件邮箱' }]}>
          <Input placeholder="name@qq.com" />
        </Form.Item>
        <Form.Item name="imap_host" label="IMAP 服务器" rules={[{ required: true, message: '请填写 IMAP 服务器' }]}>
          <Input />
        </Form.Item>
        <Form.Item name="imap_port" label="SSL 端口" rules={[{ required: true, message: '请填写端口' }]}>
          <InputNumber min={1} max={65535} style={{ width: '100%' }} />
        </Form.Item>
        <Form.Item name="authorization_code" label="邮箱授权码" rules={[{ required: true, message: '请填写授权码' }]}>
          <Input.Password autoComplete="off" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
