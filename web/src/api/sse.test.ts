// SSE 客户端单元测试：mock fetch，用分段 ReadableStream 模拟网络分片
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { postChatSSE, type SSEHandlers } from './sse'

// 把 SSE 文本分片编码成流式响应体（每片一次 enqueue，模拟网络分包）
function makeStreamResponse(chunks: string[]): Response {
  const encoder = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk))
      controller.close()
    },
  })
  return { ok: true, status: 200, body } as unknown as Response
}

describe('postChatSSE', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('token', 'test-token')
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('各事件正确派发到对应 handler 且 JSON 解析正确', async () => {
    const events: Record<string, unknown[]> = {
      meta: [],
      think: [],
      delta: [],
      tool_start: [],
      tool_end: [],
      note_updated: [],
      done: [],
      error: [],
    }
    const handlers: SSEHandlers = {
      onMeta: (d) => events.meta.push(d),
      onThink: (d) => events.think.push(d),
      onDelta: (d) => events.delta.push(d),
      onToolStart: (d) => events.tool_start.push(d),
      onToolEnd: (d) => events.tool_end.push(d),
      onNoteUpdated: (d) => events.note_updated.push(d),
      onDone: (d) => events.done.push(d),
      onError: (d) => events.error.push(d),
    }
    const sseText =
      [
        'event: meta\ndata: {"conversation_id":"c1","user_message_id":"u2"}',
        'event: think\ndata: {"content":"思考一下"}',
        'event: delta\ndata: {"content":"正文片段"}',
        'event: tool_start\ndata: {"id":"t1","name":"search_notes","input":{"q":"x"}}',
        'event: tool_end\ndata: {"id":"t1","name":"search_notes","ok":true,"summary":"找到 2 条笔记","result":"{\\"total\\":2}"}',
        'event: note_updated\ndata: {"note_id":"n1"}',
        'event: done\ndata: {"conversation_id":"c1"}',
        'event: error\ndata: {"message":"出错了"}',
      ].join('\n\n') + '\n\n'
    // 整段也故意切成三块，顺便覆盖分包
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => makeStreamResponse([sseText.slice(0, 30), sseText.slice(30, 200), sseText.slice(200)])),
    )

    await postChatSSE({ content: 'hi' }, handlers)

    expect(events.meta).toEqual([{ conversation_id: 'c1', user_message_id: 'u2' }])
    expect(events.think).toEqual([{ content: '思考一下' }])
    expect(events.delta).toEqual([{ content: '正文片段' }])
    expect(events.tool_start).toEqual([{ id: 't1', name: 'search_notes', input: { q: 'x' } }])
    expect(events.tool_end).toEqual([
      { id: 't1', name: 'search_notes', ok: true, summary: '找到 2 条笔记', result: '{"total":2}' },
    ])
    expect(events.note_updated).toEqual([{ note_id: 'n1' }])
    expect(events.done).toEqual([{ conversation_id: 'c1' }])
    expect(events.error).toEqual([{ message: '出错了' }])
  })

  it('跨 chunk 的事件块能拼接完整', async () => {
    const full = 'event: delta\ndata: {"content":"拼接完成"}\n\n'
    // 从事件块中间（event 行 / data 行内部）切开
    const chunks = [full.slice(0, 5), full.slice(5, 17), full.slice(17)]
    const deltas: string[] = []
    vi.stubGlobal('fetch', vi.fn(async () => makeStreamResponse(chunks)))

    await postChatSSE({ content: 'hi' }, { onDelta: (d) => deltas.push(d.content) })

    expect(deltas).toEqual(['拼接完成'])
  })

  it('坏 JSON 的 data 行被静默忽略（不抛错、不派发）', async () => {
    const deltas: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        makeStreamResponse(['event: delta\ndata: {坏掉的json\n\n', 'event: delta\ndata: {"content":"正常"}\n\n']),
      ),
    )

    await postChatSSE({ content: 'hi' }, { onDelta: (d) => deltas.push(d.content) })

    expect(deltas).toEqual(['正常'])
  })

  it('非 200 响应通过 onError 回报状态码与错误文本', async () => {
    const errors: string[] = []
    vi.stubGlobal(
      'fetch',
      vi.fn(
        async () =>
          ({
            ok: false,
            status: 500,
            text: async () => '服务器开小差了',
          }) as unknown as Response,
      ),
    )

    await postChatSSE({ content: 'hi' }, { onError: (d) => errors.push(d.message) })

    expect(errors).toEqual(['请求失败 500: 服务器开小差了'])
  })

  it('401 响应清除本地 token', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 401 }) as unknown as Response))

    try {
      await postChatSSE({ content: 'hi' }, {})
    } catch {
      // happy-dom 下 location.href 赋值可能抛 "Not implemented: navigation"，此处只关心 token 已清
    }

    expect(localStorage.getItem('token')).toBeNull()
  })
})
