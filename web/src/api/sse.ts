// 基于 fetch 的 SSE 客户端（POST 请求 + 事件流解析）

export interface SSEHandlers {
  onMeta?: (data: { conversation_id: string; user_message_id: string }) => void
  onDelta?: (data: { content: string }) => void
  onToolStart?: (data: { name: string; input: unknown }) => void
  onToolEnd?: (data: { name: string; ok: boolean; summary: string }) => void
  onNoteUpdated?: (data: { note_id: string }) => void
  onDone?: (data: { conversation_id: string }) => void
  onError?: (data: { message: string }) => void
}

export interface ChatRequest {
  conversation_id?: string
  note_id?: string
  content: string
}

export async function postChatSSE(body: ChatRequest, handlers: SSEHandlers): Promise<void> {
  const token = localStorage.getItem('token')
  const resp = await fetch('/api/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(body),
  })

  if (!resp.ok) {
    if (resp.status === 401) {
      localStorage.removeItem('token')
      location.href = '/login'
      return
    }
    const text = await resp.text()
    handlers.onError?.({ message: `请求失败 ${resp.status}: ${text}` })
    return
  }

  const reader = resp.body?.getReader()
  if (!reader) {
    handlers.onError?.({ message: '浏览器不支持流式响应' })
    return
  }

  const decoder = new TextDecoder()
  let buffer = ''

  const dispatch = (event: string, dataRaw: string) => {
    let data: any
    try {
      data = JSON.parse(dataRaw)
    } catch {
      return
    }
    switch (event) {
      case 'meta': handlers.onMeta?.(data); break
      case 'delta': handlers.onDelta?.(data); break
      case 'tool_start': handlers.onToolStart?.(data); break
      case 'tool_end': handlers.onToolEnd?.(data); break
      case 'note_updated': handlers.onNoteUpdated?.(data); break
      case 'done': handlers.onDone?.(data); break
      case 'error': handlers.onError?.(data); break
    }
  }

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    // SSE 事件以空行分隔
    let idx: number
    while ((idx = buffer.indexOf('\n\n')) !== -1) {
      const block = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)

      let event = ''
      let data = ''
      for (const line of block.split('\n')) {
        if (line.startsWith('event:')) event = line.slice(6).trim()
        else if (line.startsWith('data:')) data += line.slice(5).trim()
      }
      if (event && data) dispatch(event, data)
    }
  }
}
