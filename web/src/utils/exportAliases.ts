import type { AccountSummary, Alias } from '../api/types'

function filenamePart(value: string): string {
  return value.replace(/[<>:"/\\|?*]/g, '_').replace(/\p{Cc}/gu, '_')
    .trim().replace(/[. ]+$/g, '').slice(0, 80) || 'account'
}

export function createAliasExport(account: AccountSummary, aliases: Alias[]) {
  return {
    filename: `icloud-hme_${filenamePart(account.name)}_${filenamePart(account.id)}_${new Date().toISOString().slice(0, 10)}.txt`,
    content: aliases.map((alias) => alias.email).join('\r\n') + (aliases.length ? '\r\n' : ''),
  }
}

export function downloadAliasExport(account: AccountSummary, aliases: Alias[]): void {
  const { filename, content } = createAliasExport(account, aliases)
  const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  try {
    link.click()
  } finally {
    link.remove()
    // 给浏览器留出启动下载的时间，再释放临时 URL。
    window.setTimeout(() => URL.revokeObjectURL(url), 1000)
  }
}
