// 基于 fetch 的 SSE 客户端（POST 请求 + 事件流解析）

import router from '../router'

export interface SSEHandlers {
  onMeta?: (data: { conversation_id: string; user_message_id: string }) => void
  onDelta?: (data: { content: string }) => void
  onThink?: (data: { content: string }) => void
  onToolStart?: (data: { id: string; name: string; input: unknown }) => void
  onToolEnd?: (data: { id: string; name: string; ok: boolean; summary: string; result?: string }) => void
  onNoteUpdated?: (data: { note_id: string }) => void
  // AI 正文修改提案（不落库，用户审核后由前端保存）
  onNoteProposal?: (data: { note_id: string; tool: string; content: string }) => void
  onDone?: (data: { conversation_id: string }) => void
  onError?: (data: { message: string }) => void
}

export interface ChatRequest {
  conversation_id?: string
  note_id?: string
  content: string
}

export async function postChatSSE(body: ChatRequest, handlers: SSEHandlers, signal?: AbortSignal): Promise<void> {
  const token = localStorage.getItem('token')
  const resp = await fetch('/api/chat', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
    },
    body: JSON.stringify(body),
    signal,
  })

  if (!resp.ok) {
    if (resp.status === 401) {
      localStorage.removeItem('token')
      // 用 router 跳转（不整页刷新），避免丢失编辑器未保存内容
      if (router.currentRoute.value.name !== 'login') router.push('/login').catch(() => {})
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
      case 'think': handlers.onThink?.(data); break
      case 'tool_start': handlers.onToolStart?.(data); break
      case 'tool_end': handlers.onToolEnd?.(data); break
      case 'note_updated': handlers.onNoteUpdated?.(data); break
      case 'note_proposal': handlers.onNoteProposal?.(data); break
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
