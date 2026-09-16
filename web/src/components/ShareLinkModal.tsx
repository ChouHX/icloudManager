import { useEffect, useState } from 'react'
import { Alert, App as AntdApp, Button, Modal, Popconfirm, Space, Spin, Typography } from 'antd'
import { DeleteOutlined } from '@ant-design/icons'
import { request } from '../api/client'
import { describeError } from '../api/hooks'
import type { Alias, ShareLink } from '../api/types'

interface Props {
  accountId: string
  alias: Alias
  onClose: () => void
  /** 撤销后通知父组件刷新别名列表 */
  onChanged: () => void
}

interface State {
  loading: boolean
  link: ShareLink | null
  error: string
}

/**
 * 展示某个别名的取件链接。
 *
 * 打开时调用生成接口(幂等):已有链接会直接返回同一个 token,
 * 因此这个弹窗既是"生成"也是"查看/撤销"的入口。
 */
export default function ShareLinkModal({ accountId, alias, onClose, onChanged }: Props) {
  const { message } = AntdApp.useApp()
  const [state, setState] = useState<State>({ loading: true, link: null, error: '' })

  useEffect(() => {
    let cancelled = false
    request<ShareLink>(`/api/aliases/${encodeURIComponent(alias.anonymousId)}/share-link`, {
      method: 'POST',
      body: { account_id: accountId },
    })
      .then((data) => {
        if (!cancelled) setState({ loading: false, link: data, error: '' })
      })
      .catch((err) => {
        if (!cancelled) setState({ loading: false, link: null, error: describeError(err) })
      })
    return () => {
      cancelled = true
    }
  }, [accountId, alias.anonymousId])

  async function revoke() {
    setState((prev) => ({ ...prev, loading: true }))
    try {
      await request(`/api/aliases/${encodeURIComponent(alias.anonymousId)}/share-link`, {
        method: 'DELETE',
        body: { account_id: accountId },
      })
      message.success('取件链接已撤销')
      onChanged()
      onClose()
    } catch (err) {
      setState((prev) => ({ ...prev, loading: false, error: describeError(err) }))
    }
  }

  return (
    <Modal
      open
      title="取件链接"
      onCancel={onClose}
      destroyOnHidden
      footer={
        <Space>
          <Popconfirm
            title="撤销这个取件链接？"
            description="撤销后该链接立即失效，重新生成会得到新的链接。"
            okText="撤销"
            okButtonProps={{ danger: true }}
            cancelText="取消"
            onConfirm={() => void revoke()}
          >
            <Button danger icon={<DeleteOutlined />} disabled={!state.link}>
              撤销链接
            </Button>
          </Popconfirm>
          <Button type="primary" onClick={onClose}>
            完成
          </Button>
        </Space>
      }
    >
      {state.error && <Alert type="error" showIcon title={state.error} style={{ marginBottom: 16 }} />}

      {state.loading ? (
        <div style={{ textAlign: 'center', padding: '24px 0' }}>
          <Spin tip="生成中…" />
        </div>
      ) : (
        state.link && (
          <>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
              别名：<code>{state.link.alias}</code>
            </Typography.Paragraph>
            <div className="created-address" style={{ alignItems: 'flex-start', flexDirection: 'column', gap: 6 }}>
              <Typography.Text className="mono" copyable={{ text: state.link.url }}>
                {state.link.url}
              </Typography.Text>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                点击链接文本即可复制
              </Typography.Text>
            </div>
            <Alert
              type="info"
              showIcon
              title="持有该链接的人可以只读查看这个别名收到的邮件"
              description="链接不需要登录，只能看到这一个别名，不能访问其它别名或账号设置；可随时在这里撤销。"
              style={{ marginTop: 16 }}
            />
          </>
        )
      )}
    </Modal>
  )
}
