import { useState } from 'react'
import { Alert, Button, Card, Form, Input, Typography } from 'antd'
import { LockOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'
import { ApiError } from '../api/client'

export default function LoginPage() {
  const { login } = useAuth()
  const navigate = useNavigate()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  async function handleFinish(values: { password: string }) {
    setSubmitting(true)
    setError('')
    try {
      await login(values.password)
      navigate('/aliases', { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '网络连接失败，请检查服务状态')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login-screen">
      <div className="login-card">
        <div className="login-brand">
          <span className="app-brand-mark" aria-hidden="true">
            <SafetyCertificateOutlined />
          </span>
          <h1>iCloud 隐私邮箱</h1>
          <p>创建 Hide My Email 别名，随时取件</p>
        </div>

        <Card variant="borderless">
          {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} />}
          <Form layout="vertical" onFinish={(values) => void handleFinish(values)} requiredMark={false}>
            <Form.Item
              name="password"
              label="管理员密码"
              rules={[{ required: true, message: '请输入管理员密码' }]}
            >
              <Input.Password
                prefix={<LockOutlined />}
                autoComplete="current-password"
                placeholder="请输入管理员密码"
                size="large"
              />
            </Form.Item>
            <Button type="primary" htmlType="submit" block size="large" loading={submitting}>
              登录
            </Button>
          </Form>
        </Card>

        <Typography.Paragraph type="secondary" style={{ textAlign: 'center', marginTop: 16, fontSize: 12 }}>
          密码来自启动时的 <code>ICLOUD_HME_ADMIN_PASSWORD</code> 环境变量
        </Typography.Paragraph>
      </div>
    </div>
  )
}
