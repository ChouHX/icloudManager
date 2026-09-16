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
