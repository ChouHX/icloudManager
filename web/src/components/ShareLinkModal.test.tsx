import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import ShareLinkModal from './ShareLinkModal'
import { ALIASES } from '../test/handlers'
import { renderPage } from '../test/render'

function renderModal(onChanged = vi.fn(), onClose = vi.fn()) {
  return renderPage(
    <ShareLinkModal accountId="acc_1" alias={ALIASES[0]} onClose={onClose} onChanged={onChanged} />,
    { route: '/aliases', path: '/aliases' },
  )
}

describe('ShareLinkModal', () => {
  it('生成并展示取件链接', async () => {
    renderModal()

    expect(await screen.findByText(/http:\/\/localhost:8081\/\?token=tok_test_token/)).toBeInTheDocument()
    expect(screen.getByText(/持有该链接的人可以只读查看这个别名收到的邮件/)).toBeInTheDocument()
    expect(screen.getByText('alpha@icloud.com')).toBeInTheDocument()
  })

  it('撤销链接后通知父组件并关闭', async () => {
    const user = userEvent.setup()
    const onChanged = vi.fn()
    const onClose = vi.fn()
    renderModal(onChanged, onClose)
    await screen.findByText(/token=tok_test_token/)

    await user.click(screen.getByRole('button', { name: /撤销链接/ }))
    await user.click(await screen.findByRole('button', { name: /撤\s*销$/ }))

    expect(onChanged).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalled()
  })
})
