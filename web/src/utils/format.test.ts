import { describe, expect, it } from 'vitest'
import { sharePercent } from './format'

describe('sharePercent', () => {
  it('以记录总数为分母计算占比', () => {
    // 3000 个号里有 1000 个用了某标签 → 33.3%
    expect(sharePercent(1000, 3000)).toBe(33.3)
    expect(sharePercent(18470, 35526)).toBe(52)
    expect(sharePercent(18795, 35526)).toBe(52.9)
  })

  it('保留一位小数并封顶 100%', () => {
    expect(sharePercent(1, 3)).toBe(33.3)
    expect(sharePercent(2, 3)).toBe(66.7)
    expect(sharePercent(3000, 3000)).toBe(100)
    expect(sharePercent(4000, 3000)).toBe(100)
  })

  it('分母为 0 或负数时返回 0', () => {
    expect(sharePercent(10, 0)).toBe(0)
    expect(sharePercent(10, -1)).toBe(0)
    expect(sharePercent(0, 100)).toBe(0)
  })
})
