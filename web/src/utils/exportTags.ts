import type { TagEntry } from '../api/types'

function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

/** 下载 JSON 文件(备份/迁移用) */
export function downloadJSON(payload: unknown, filename: string): void {
  downloadBlob(new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' }), filename)
}

/** 生成备份文件名 */
export function backupFileName(exportedAt: string): string {
  return `tags-backup-${fileStamp(exportedAt)}.json`
}

/** 生成批量导出文件名 */
export function exportFileName(exportedAt: string, ext: 'csv' | 'json'): string {
  return `tags-export-${fileStamp(exportedAt)}.${ext}`
}

function fileStamp(exportedAt: string): string {
  return (exportedAt || new Date().toISOString())
    .replace(/[:.]/g, '-')
    .replace(/\+.*$/, '')
}

function csvCell(value: string): string {
  return `"${value.replace(/"/g, '""')}"`
}

/** 将标签凭据转换为 CSV,包含表头并处理引号、逗号与换行 */
export function entriesToCSV(entries: TagEntry[]): string {
  const header = ['邮箱', '密码', '网站', '项目标签', '备注', '创建时间', '更新时间']
  const lines = [header.map(csvCell).join(',')]
  for (const entry of entries) {
    const sites = entry.sites?.length ? entry.sites : entry.site ? [entry.site] : []
    lines.push(
      [
        entry.email,
        entry.password ?? '',
        sites.join('、'),
        (entry.tags ?? []).join('、'),
        entry.note ?? '',
        entry.created_at ?? '',
        entry.updated_at ?? '',
      ]
        .map(csvCell)
        .join(','),
    )
  }
  return lines.join('\r\n')
}

/** 下载带 UTF-8 BOM 的 CSV,便于 Excel/WPS 正确识别中文 */
export function downloadCSV(entries: TagEntry[], filename: string): void {
  const csv = `\uFEFF${entriesToCSV(entries)}`
  downloadBlob(new Blob([csv], { type: 'text/csv;charset=utf-8' }), filename)
}
