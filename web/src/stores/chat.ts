import { defineStore } from 'pinia'
import api from '../api/client'
import { postChatSSE } from '../api/sse'

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant' | 'tool'
  content: string
  tool_calls?: string
  tool_call_id?: string
  name?: string
  // 前端附加状态
  streaming?: boolean
  toolSummary?: string
  toolOk?: boolean
  toolInput?: unknown
}

export interface Conversation {
  id: string
  note_id: string | null
  title: string
  updated_at: string
}

// 每个面板（global / note:<id>）一份独立聊天状态
export const useChatStore = defineStore('chat', {
  state: () => ({
    conversations: [] as Conversation[],
    currentId: '' as string,
    messages: [] as ChatMessage[],
    sending: false,
    scopeNoteId: '' as string, // '' = 全局会话；否则绑定笔记
    noteUpdatedFlag: 0, // 编辑器同步信号（自增）
    lastUpdatedNoteId: '' as string,
  }),
  actions: {
    // 切换作用域（进入首页 / 编辑页时调用）
    async switchScope(noteId: string) {
      this.scopeNoteId = noteId
      this.currentId = ''
      this.messages = []
      await this.fetchConversations()
      // 自动选中最近的会话
      if (this.conversations.length > 0) {
        await this.select(this.conversations[0].id)
      }
    },

    async fetchConversations() {
      const params: Record<string, string> = {}
      if (this.scopeNoteId) params.note_id = this.scopeNoteId
      const { data } = await api.get('/conversations', { params })
      this.conversations = data || []
    },

    async select(convId: string) {
      this.currentId = convId
      const { data } = await api.get(`/conversations/${convId}/messages`)
      // tool 消息折叠为摘要行展示
      this.messages = (data || []).map((m: any) => ({
        ...m,
        toolSummary: m.role === 'tool' ? '工具调用结果' : undefined,
        toolOk: true,
      }))
    },

    async newConversation() {
      this.currentId = ''
      this.messages = []
    },

    async removeConversation(convId: string) {
      await api.post('/conversations/delete', { id: convId })
      if (this.currentId === convId) {
        this.currentId = ''
        this.messages = []
      }
      await this.fetchConversations()
    },

    async send(content: string) {
      if (this.sending || !content.trim()) return
      this.sending = true

      // 乐观插入用户消息
      this.messages.push({
        id: `tmp-u-${Date.now()}`,
        role: 'user',
        content: content.trim(),
      })
      // 插入流式 assistant 占位
      const assistantMsg: ChatMessage = {
        id: `tmp-a-${Date.now()}`,
        role: 'assistant',
        content: '',
        streaming: true,
      }
      this.messages.push(assistantMsg)

      const body: Parameters<typeof postChatSSE>[0] = { content: content.trim() }
      if (this.currentId) body.conversation_id = this.currentId
      else if (this.scopeNoteId) body.note_id = this.scopeNoteId

      let errored = false

      try {
        await postChatSSE(body, {
          onMeta: (d) => {
            this.currentId = d.conversation_id
            // 会话列表刷新（新会话）
            if (!this.conversations.find((cv) => cv.id === d.conversation_id)) {
              this.fetchConversations()
            }
          },
          onDelta: (d) => {
            assistantMsg.content += d.content
          },
          onToolStart: (d) => {
            // 在 assistant 消息前插入工具状态行
            const toolMsg: ChatMessage = {
              id: `tool-${Date.now()}-${Math.random()}`,
              role: 'tool',
              content: '',
              name: d.name,
              toolInput: d.input,
              toolSummary: toolPendingText(d.name),
              streaming: true,
            }
            // 插入到 assistant 占位之前
            const idx = this.messages.indexOf(assistantMsg)
            this.messages.splice(idx, 0, toolMsg)
          },
          onToolEnd: (d) => {
            // 找到最后一个该名字的进行中工具消息
            for (let i = this.messages.length - 1; i >= 0; i--) {
              const m = this.messages[i]
              if (m.role === 'tool' && m.name === d.name && m.streaming) {
                m.streaming = false
                m.toolOk = d.ok
                m.toolSummary = d.summary
                break
              }
            }
          },
          onNoteUpdated: (d) => {
            this.lastUpdatedNoteId = d.note_id
            this.noteUpdatedFlag++
          },
          onDone: () => {
            assistantMsg.streaming = false
            this.fetchConversations() // 更新会话排序/标题
          },
          onError: (d) => {
            errored = true
            assistantMsg.streaming = false
            assistantMsg.content = assistantMsg.content
              ? assistantMsg.content + `\n\n⚠ ${d.message}`
              : `⚠ ${d.message}`
          },
        })
      } catch (e: any) {
        errored = true
        assistantMsg.streaming = false
        assistantMsg.content = `⚠ 网络错误：${e.message || '连接中断'}`
      } finally {
        this.sending = false
        if (errored && !assistantMsg.content) {
          assistantMsg.content = '⚠ 对话出错，请重试'
        }
      }
    },
  },
})

function toolPendingText(name: string): string {
  switch (name) {
    case 'search_notes': return '正在搜索笔记…'
    case 'get_note': return '正在读取笔记…'
    case 'list_categories': return '正在获取分类…'
    case 'replace_note_section': return '正在修改笔记…'
    case 'append_note_content': return '正在追加内容…'
    case 'update_note_title': return '正在修改标题…'
    case 'create_note': return '正在创建笔记…'
    default: return `正在执行 ${name}…`
  }
}
