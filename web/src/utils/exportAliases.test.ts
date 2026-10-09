import { describe, expect, it } from 'vitest'
import { ACCOUNT, ALIASES, SECOND_ACCOUNT } from '../test/handlers'
import { createAliasExport } from './exportAliases'

describe('createAliasExport', () => {
  it('每行仅包含一个邮箱地址，包含启用及停用别名', () => {
    const { content } = createAliasExport(
      { ...ACCOUNT, name: '主号,"工作"' },
      [{ ...ALIASES[0], label: '购物,"订阅"\n第二行' }, ALIASES[1]],
    )

    expect(content).toBe('alpha@icloud.com\r\nbeta@icloud.com\r\n')
  })

  it('空列表生成空文本', () => {
    expect(createAliasExport(ACCOUNT, []).content).toBe('')
  })

  it('同名账号的文件名仍可区分，移除文件名中的非法字符', () => {
    const name = '工作/备份\\账号:*?"<>|\n'
    const first = createAliasExport({ ...ACCOUNT, name }, ALIASES)
    const second = createAliasExport({ ...SECOND_ACCOUNT, name }, ALIASES)

    expect(first.filename).not.toMatch(/[<>:"/\\|?*\n]/)
    expect(first.filename).toMatch(/^icloud-hme_工作_备份_账号.*_acc_1_\d{4}-\d{2}-\d{2}\.txt$/)
    expect(second.filename).toContain('_acc_2_')
    expect(first.filename).not.toBe(second.filename)
  })
})
