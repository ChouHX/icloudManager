import { describe, expect, it } from 'vitest'
import type { TagEntry } from '../api/types'
import { entriesToCSV } from './exportTags'

describe('entriesToCSV', () => {
  it('生成表头、标签列并转义引号与换行', () => {
    const entries: TagEntry[] = [
      {
        id: 'tag_1',
        email: 'user@example.com',
        password: 'p"1',
        site: 'GitHub',
        tags: ['项目A', '开发'],
        note: '第一行\n第二行',
        created_at: '2026-08-01T09:00:00+08:00',
        updated_at: '2026-08-02T09:00:00+08:00',
      },
    ]

    const csv = entriesToCSV(entries)
    const lines = csv.split('\r\n')

    expect(lines[0]).toBe('"邮箱","密码","网站","项目标签","备注","创建时间","更新时间"')
    expect(lines[1]).toContain('"user@example.com"')
    expect(lines[1]).toContain('"p""1"')
    expect(lines[1]).toContain('"项目A、开发"')
    expect(csv).toContain('"第一行\n第二行"')
  })
})
