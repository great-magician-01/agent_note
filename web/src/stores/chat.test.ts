// 聊天 store 与视图构建逻辑的单元测试
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { toolLabel, toolPendingText, summarizeToolContent, buildViewItems, useChatStore } from './chat'
import type { ChatMessage } from './chat'
import { postChatSSE } from '../api/sse'
import type { SSEHandlers } from '../api/sse'

// mock 掉 axios 实例与 SSE 客户端，避免真实网络请求
vi.mock('../api/client', () => ({
  default: {
    get: vi.fn(async () => ({ data: [] })),
    post: vi.fn(async () => ({ data: {} })),
  },
}))
vi.mock('../api/sse', () => ({
  postChatSSE: vi.fn(),
}))

describe('toolLabel', () => {
  it('已知工具名返回中文标签', () => {
    expect(toolLabel('search_notes')).toBe('搜索笔记')
    expect(toolLabel('get_note')).toBe('读取笔记')
  })

  it('未知工具名原样返回', () => {
    expect(toolLabel('some_unknown_tool')).toBe('some_unknown_tool')
  })

  it('未传名字返回「工具」', () => {
    expect(toolLabel(undefined)).toBe('工具')
    expect(toolLabel('')).toBe('工具')
  })
})

describe('toolPendingText', () => {
  it('拼接进行中文案', () => {
    expect(toolPendingText('search_notes')).toBe('正在搜索笔记…')
    expect(toolPendingText('xyz')).toBe('正在xyz…')
  })
})

describe('summarizeToolContent', () => {
  it('content 含 error 字段时 ok:false 且 summary 取 error', () => {
    expect(summarizeToolContent('get_note', '{"error":"笔记不存在"}')).toEqual({
      ok: false,
      summary: '笔记不存在',
    })
  })

  it('search_notes 带 total 时给出条数', () => {
    expect(summarizeToolContent('search_notes', '{"total":5}')).toEqual({
      ok: true,
      summary: '找到 5 条笔记',
    })
  })

  it('具名分支：get_note / list_categories 等', () => {
    expect(summarizeToolContent('get_note', '{}')).toEqual({ ok: true, summary: '已读取笔记全文' })
    expect(summarizeToolContent('list_categories', '[]')).toEqual({ ok: true, summary: '已获取分类列表' })
    expect(summarizeToolContent('replace_note_section', '{}')).toEqual({ ok: true, summary: '已替换笔记内容' })
    expect(summarizeToolContent('append_note_content', '{}')).toEqual({ ok: true, summary: '已追加内容到笔记' })
    expect(summarizeToolContent('update_note_title', '{}')).toEqual({ ok: true, summary: '已修改笔记标题' })
    expect(summarizeToolContent('create_note', '{}')).toEqual({ ok: true, summary: '已创建新笔记' })
  })

  it('坏 JSON 走 switch 兜底', () => {
    // 未知工具名 + 非 JSON → default 分支
    expect(summarizeToolContent('whatever', 'not json')).toEqual({ ok: true, summary: '工具执行完成' })
    // 已知工具名 + 非 JSON → 具名分支取不到 total，显示 ?
    expect(summarizeToolContent('search_notes', 'not json')).toEqual({ ok: true, summary: '找到 ? 条笔记' })
  })
})

describe('buildViewItems', () => {
  it('user 消息直出', () => {
    const items = buildViewItems([{ id: 'u1', role: 'user', content: '你好' }])
    expect(items).toEqual([{ key: 'u1', kind: 'user', content: '你好' }])
  })

  it('assistant 带 reasoning + content + tool_calls 时依次展开，tool 消息按序消费', () => {
    const toolCalls = JSON.stringify([
      { id: 't1', type: 'function', function: { name: 'search_notes', arguments: '{"q":"笔记"}' } },
    ])
    const messages: ChatMessage[] = [
      { id: 'u1', role: 'user', content: '找一下' },
      { id: 'a1', role: 'assistant', content: '找到了', reasoning: '先检索', tool_calls: toolCalls },
      { id: 'm1', role: 'tool', content: '{"total":3}', tool_call_id: 't1', name: 'search_notes' },
    ]
    const items = buildViewItems(messages)

    expect(items.map((i) => i.kind)).toEqual(['user', 'think', 'text', 'tool'])
    expect(items[1]).toMatchObject({ key: 'a1-think', content: '先检索' })
    expect(items[2]).toMatchObject({ key: 'a1-text', content: '找到了' })
    expect(items[3]).toMatchObject({
      key: 'a1-tc-t1',
      name: 'search_notes',
      ok: true,
      summary: '找到 3 条笔记',
      result: '{"total":3}',
    })
    // 紧随的 tool 消息被消费，不再单独出现
    expect(items).toHaveLength(4)
  })

  it('tool_call_id 对不上的 tool 消息作为孤儿项兜底展示', () => {
    const toolCalls = JSON.stringify([
      { id: 't1', type: 'function', function: { name: 'get_note', arguments: '{}' } },
    ])
    const messages: ChatMessage[] = [
      { id: 'a1', role: 'assistant', content: '', tool_calls: toolCalls },
      { id: 'm9', role: 'tool', content: '{"error":"未找到笔记"}', tool_call_id: 'other', name: 'get_note' },
    ]
    const items = buildViewItems(messages)

    // assistant 无正文无推理 → 仅一个结果缺失的 tool 项；孤儿消息紧随其后兜底展示
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({ key: 'a1-tc-t1', kind: 'tool', summary: '结果缺失' })
    expect(items[1]).toMatchObject({ key: 'm9', kind: 'tool', ok: false, summary: '未找到笔记' })
  })
})

describe('chat store send 流程', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('meta/delta/tool/error 回调正确更新状态，完成后 sending 复位', async () => {
    // postChatSSE 捕获 handlers 后挂起，由测试手动同步回放事件
    let handlers: SSEHandlers | undefined
    let release: () => void = () => {}
    vi.mocked(postChatSSE).mockImplementation((_body, h) => {
      handlers = h
      return new Promise<void>((resolve) => {
        release = resolve
      })
    })

    const store = useChatStore()
    const p = store.send('你好')
    expect(store.sending).toBe(true)

    // onMeta：会话 ID 更新
    handlers!.onMeta?.({ conversation_id: '1', user_message_id: '2' })
    expect(store.currentId).toBe('1')

    // onDelta 两次：assistant 正文累积
    handlers!.onDelta?.({ content: '你' })
    handlers!.onDelta?.({ content: '好' })
    const assistant = () => store.messages.filter((m) => m.role === 'assistant')
    expect(assistant()[0].content).toBe('你好')

    // onToolStart：出现 tool 消息，摘要为 pending 文案
    handlers!.onToolStart?.({ id: 't1', name: 'search_notes', input: { q: 'x' } })
    const toolMsg = () => store.messages.find((m) => m.role === 'tool')
    expect(toolMsg()?.toolSummary).toBe('正在搜索笔记…')
    expect(toolMsg()?.streaming).toBe(true)

    // onToolEnd：摘要变为结果文案
    handlers!.onToolEnd?.({ id: 't1', name: 'search_notes', ok: true, summary: '找到 3 条笔记', result: '{"total":3}' })
    expect(toolMsg()?.toolSummary).toBe('找到 3 条笔记')
    expect(toolMsg()?.toolOk).toBe(true)
    expect(toolMsg()?.streaming).toBe(false)

    // onError：新的 assistant 消息 content 含 ⚠
    handlers!.onError?.({ message: '出错了' })
    const last = store.messages[store.messages.length - 1]
    expect(last.role).toBe('assistant')
    expect(last.content).toContain('⚠')

    handlers!.onDone?.({ conversation_id: '1' })
    release()
    await p

    expect(store.sending).toBe(false)
    // 用户消息乐观插入在首位
    expect(store.messages[0]).toMatchObject({ role: 'user', content: '你好' })
    // 请求体不带会话/笔记 ID（新全局会话）
    expect(vi.mocked(postChatSSE).mock.calls[0][0]).toEqual({ content: '你好' })
  })

  it('note_proposal 事件写入 pendingProposal，clearProposal 清空', async () => {
    let handlers: SSEHandlers | undefined
    let release: () => void = () => {}
    vi.mocked(postChatSSE).mockImplementation((_body, h) => {
      handlers = h
      return new Promise<void>((resolve) => {
        release = resolve
      })
    })

    const store = useChatStore()
    const p = store.send('帮我改一下')

    handlers!.onNoteProposal?.({ note_id: 'n1', tool: 'replace_note_section', content: '旧提案' })
    expect(store.pendingProposal).toEqual({ noteId: 'n1', tool: 'replace_note_section', content: '旧提案' })

    // 新提案覆盖旧提案
    handlers!.onNoteProposal?.({ note_id: 'n1', tool: 'append_note_content', content: '新提案' })
    expect(store.pendingProposal?.content).toBe('新提案')

    store.clearProposal()
    expect(store.pendingProposal).toBeNull()

    release()
    await p
  })

  it('stop() 中断时把 streaming 的 tool 消息标记为「已中断」', async () => {
    let handlers: SSEHandlers | undefined
    vi.mocked(postChatSSE).mockImplementation((_body, h) => {
      handlers = h
      return new Promise<void>((_resolve, reject) => {
        // abort 时模拟 fetch 抛 AbortError
        const err = new Error('aborted')
        err.name = 'AbortError'
        // postChatSSE 内部监听不到 signal 时由调用方 reject，这里直接挂起，由 stop 后的 finally 收尾
        setTimeout(() => reject(err), 0)
      })
    })

    const store = useChatStore()
    const p = store.send('hi')
    handlers!.onToolStart?.({ id: 't1', name: 'search_notes', input: {} })
    expect(store.messages.find((m) => m.role === 'tool')?.streaming).toBe(true)

    store.stop()
    await p

    const toolMsg = store.messages.find((m) => m.role === 'tool')
    expect(toolMsg?.streaming).toBe(false)
    expect(toolMsg?.toolSummary).toBe('已中断')
    expect(toolMsg?.toolOk).toBe(false)
  })
})
