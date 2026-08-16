// useTitle 单元测试：验证浏览器标题拼接规则
import { describe, it, expect } from 'vitest'
import { setTitle } from './useTitle'

describe('setTitle', () => {
  it('无子标题时使用基础标题', () => {
    setTitle()
    expect(document.title).toBe('AI 智能笔记')
  })

  it('有子标题时拼接「子标题 · AI 智能笔记」', () => {
    setTitle('设置')
    expect(document.title).toBe('设置 · AI 智能笔记')
  })
})
