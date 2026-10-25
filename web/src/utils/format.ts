import dayjs from 'dayjs'

/**
 * 解析 iCloud 返回的时间值。
 * iCloud 可能返回 ISO 字符串，也可能返回秒、毫秒或微秒时间戳。
 */
export function parseTimestamp(raw?: string): dayjs.Dayjs | null {
  const value = raw?.trim()
  if (!value) return null

  if (/^[+-]?\d+(?:\.\d+)?$/.test(value)) {
    const numeric = Number(value)
    if (Number.isFinite(numeric)) {
      const magnitude = Math.abs(numeric)
      const milliseconds =
        magnitude < 1e11
          ? numeric * 1000
          : magnitude < 1e14
            ? numeric
            : magnitude < 1e17
              ? numeric / 1000
              : numeric / 1e6
      const parsed = dayjs(milliseconds)
      if (parsed.isValid()) return parsed
    }
  }

  const parsed = dayjs(value)
  return parsed.isValid() ? parsed : null
}

/** 统一列表时间显示：2026/01/15 10:30 */
export function formatDateTime(raw?: string): string {
  const parsed = parseTimestamp(raw)
  return parsed ? parsed.format('YYYY/MM/DD HH:mm') : raw?.trim() || '—'
}

/** 时间排序用数值，无法解析时返回 null */
export function timestampValue(raw?: string): number | null {
  return parseTimestamp(raw)?.valueOf() ?? null
}

/**
 * 占比：count / total，保留一位小数并封顶 100%。
 *
 * 分母必须是「记录总数」（例如 3000 个号里有 1000 个用了某标签 → 33.3%），
 * 不能用「当前表里的最大值」做归一化，否则最大值那一行恒为 100%。
 */
export function sharePercent(count: number, total: number): number {
  if (!Number.isFinite(count) || !Number.isFinite(total) || total <= 0 || count <= 0) return 0
  return Math.min(100, Math.round((count / total) * 1000) / 10)
}
