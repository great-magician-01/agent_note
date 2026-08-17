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
  reasoning?: string | null
  // 前端附加状态
  streaming?: boolean // assistant 正文仍在流入
  thinkStreaming?: boolean // 思考仍在流入
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

// ---- 消息 → 视图项（思考 / 正文 / 工具调用，按模型输出顺序） ----

export interface ChatViewItem {
  key: string
  kind: 'user' | 'think' | 'text' | 'tool'
  content: string
  streaming?: boolean
  // tool 项
  name?: string
  input?: string
  ok?: boolean
  summary?: string
  result?: string
}

const TOOL_LABELS: Record<string, string> = {
  search_notes: '搜索笔记',
  get_note: '读取笔记',
  list_all_notes: '获取全部笔记',
  list_categories: '获取分类',
  run_subagent: '委派子代理',
  replace_note_section: '修改笔记',
  append_note_content: '追加内容',
  update_note_title: '修改标题',
  create_note: '创建笔记',
}

export function toolLabel(name?: string): string {
  return (name && TOOL_LABELS[name]) || name || '工具'
}

export function toolPendingText(name: string): string {
  return `正在${toolLabel(name)}…`
}

// 从落库的 tool 消息内容推导 状态+摘要（与后端 toolSummary 对齐）
export function summarizeToolContent(name: string | undefined, content: string): { ok: boolean; summary: string } {
  let parsed: any = null
  try {
    parsed = JSON.parse(content)
  } catch {
    /* 非 JSON 内容 */
  }
  if (parsed && typeof parsed.error === 'string') return { ok: false, summary: parsed.error }
  switch (name) {
    case 'search_notes':
      return { ok: true, summary: `找到 ${parsed?.total ?? '?'} 条笔记` }
    case 'get_note':
      return { ok: true, summary: '已读取笔记全文' }
    case 'list_all_notes':
      return { ok: true, summary: `共 ${parsed?.total ?? '?'} 条笔记` }
    case 'run_subagent':
      return { ok: true, summary: '子代理已完成任务' }
    case 'list_categories':
      return { ok: true, summary: '已获取分类列表' }
    case 'replace_note_section':
      return { ok: true, summary: '已替换笔记内容' }
    case 'append_note_content':
      return { ok: true, summary: '已追加内容到笔记' }
    case 'update_note_title':
      return { ok: true, summary: '已修改笔记标题' }
    case 'create_note':
      return { ok: true, summary: '已创建新笔记' }
    default:
      return { ok: true, summary: '工具执行完成' }
  }
}

function prettyJSON(raw: string | undefined): string {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function toolView(key: string, name: string | undefined, tm: ChatMessage | undefined, input?: string): ChatViewItem {
  const derived = tm ? summarizeToolContent(name, tm.content) : { ok: true, summary: '结果缺失' }
  return {
    key,
    kind: 'tool',
    content: '',
    name,
    input,
    ok: tm?.toolOk ?? derived.ok,
    summary: tm?.toolSummary ?? derived.summary,
    result: tm?.content,
    streaming: tm?.streaming ?? false,
  }
}

// 把扁平消息列表转换为按序展示的视图项：
// 每条 assistant 消息依次展开为 思考（可折叠）→ 正文气泡 → 工具调用块（结果按序消费紧随的 tool 消息）
export function buildViewItems(messages: ChatMessage[]): ChatViewItem[] {
  const items: ChatViewItem[] = []
  let i = 0
  while (i < messages.length) {
    const m = messages[i]

    if (m.role === 'user') {
      items.push({ key: m.id, kind: 'user', content: m.content })
      i++
      continue
    }

    if (m.role === 'assistant') {
      if (m.reasoning) {
        items.push({ key: `${m.id}-think`, kind: 'think', content: m.reasoning, streaming: m.thinkStreaming })
      }
      // 有正文，或正在等待本轮首个 token（占位光标）
      const textStreaming = !!m.streaming && !m.tool_calls
      if (m.content || textStreaming) {
        items.push({ key: `${m.id}-text`, kind: 'text', content: m.content, streaming: textStreaming })
      }
      if (m.tool_calls) {
        let tcs: any[] = []
        try {
          tcs = JSON.parse(m.tool_calls)
        } catch {
          tcs = []
        }
        tcs.forEach((tc, idx) => {
          const name: string | undefined = tc.function?.name
          // 顺序消费紧随其后的 tool 消息（tool_call_id 对得上才消费，错位时兜底展示孤儿消息）
          let tm: ChatMessage | undefined
          const next = messages[i + 1]
          if (next && next.role === 'tool' && (!next.tool_call_id || !tc.id || next.tool_call_id === tc.id)) {
            tm = next
            i++
          }
          items.push(toolView(`${m.id}-tc-${tc.id || idx}`, name, tm, prettyJSON(tc.function?.arguments)))
        })
      }
      i++
      continue
    }

    // 孤儿 tool 消息（前置 assistant 缺失等异常情况）
    items.push(toolView(m.id, m.name, m, m.toolInput ? prettyJSON(JSON.stringify(m.toolInput)) : undefined))
    i++
  }
  return items
}

// 在飞请求的中断控制器（同一时间只有一个发送中请求，模块级即可）
let abortCtrl: AbortController | null = null

// AI 正文修改提案（note_proposal 事件，用户审核后由前端保存）
export interface NoteProposal {
  noteId: string
  tool: string
  content: string
}

// 把所有仍在 streaming 的 tool 消息收尾为「已中断」
function interruptStreamingTools(messages: ChatMessage[]) {
  for (const m of messages) {
    if (m.role === 'tool' && m.streaming) {
      m.streaming = false
      m.toolOk = false
      m.toolSummary = '已中断'
    }
  }
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
    pendingProposal: null as NoteProposal | null, // 待审核的 AI 正文修改提案
  }),
  actions: {
    // 中断在飞的消息（停止按钮 / 切换会话 / 卸载面板时调用）；
    // fetch 断开后后端请求 ctx 取消，上游 AI 请求随之断开
    stop() {
      abortCtrl?.abort()
      interruptStreamingTools(this.messages)
    },

    clearProposal() {
      this.pendingProposal = null
    },

    // 切换作用域（进入首页 / 编辑页时调用）
    async switchScope(noteId: string) {
      this.stop()
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
      this.stop()
      this.currentId = convId
      const { data } = await api.get(`/conversations/${convId}/messages`)
      this.messages = data || []
    },

    async newConversation() {
      this.stop()
      this.currentId = ''
      this.messages = []
    },

    async removeConversation(convId: string) {
      this.stop()
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

      // 当前轮次的 assistant 消息：每轮（思考+正文+工具调用）一条，与落库结构一致；
      // 工具调用开始时收尾本轮，下一轮首个 think/delta 再新建
      let cur: ChatMessage | null = null
      let lastAssistant: ChatMessage | null = null
      let tmpSeq = 0
      const ensureAssistant = (): ChatMessage => {
        if (!cur) {
          cur = {
            id: `tmp-a-${Date.now()}-${tmpSeq++}`,
            role: 'assistant',
            content: '',
            streaming: true,
          }
          this.messages.push(cur)
          lastAssistant = cur
        }
        return cur
      }
      const finalizeRound = () => {
        if (cur) {
          cur.streaming = false
          cur.thinkStreaming = false
          cur = null
        }
      }
      ensureAssistant() // 立即出现输入中光标

      const body: Parameters<typeof postChatSSE>[0] = { content: content.trim() }
      if (this.currentId) body.conversation_id = this.currentId
      else if (this.scopeNoteId) body.note_id = this.scopeNoteId

      let errored = false
      const ctrl = new AbortController()
      abortCtrl = ctrl

      try {
        await postChatSSE(body, {
          onMeta: (d) => {
            this.currentId = d.conversation_id
            // 会话列表刷新（新会话）
            if (!this.conversations.find((cv) => cv.id === d.conversation_id)) {
              this.fetchConversations()
            }
          },
          onThink: (d) => {
            const a = ensureAssistant()
            a.thinkStreaming = true
            a.reasoning = (a.reasoning || '') + d.content
          },
          onDelta: (d) => {
            const a = ensureAssistant()
            a.thinkStreaming = false // 正文开始 = 思考结束
            a.content += d.content
          },
          onToolStart: (d) => {
            // 工具调用记录挂到本轮 assistant 消息上（与落库的 tool_calls 结构一致）
            const a = ensureAssistant()
            a.thinkStreaming = false
            let tcs: any[] = []
            try {
              tcs = a.tool_calls ? JSON.parse(a.tool_calls) : []
            } catch {
              tcs = []
            }
            tcs.push({
              id: d.id,
              type: 'function',
              function: {
                name: d.name,
                arguments: typeof d.input === 'string' ? d.input : JSON.stringify(d.input ?? {}),
              },
            })
            a.tool_calls = JSON.stringify(tcs)
            finalizeRound()

            this.messages.push({
              id: `tool-${d.id || Date.now()}`,
              role: 'tool',
              content: '',
              tool_call_id: d.id,
              name: d.name,
              toolInput: d.input,
              toolSummary: toolPendingText(d.name),
              streaming: true,
            })
          },
          onToolEnd: (d) => {
            // 由后往前找到对应的进行中工具消息
            for (let i = this.messages.length - 1; i >= 0; i--) {
              const m = this.messages[i]
              if (m.role === 'tool' && m.streaming && (d.id ? m.tool_call_id === d.id : m.name === d.name)) {
                m.streaming = false
                m.toolOk = d.ok
                m.toolSummary = d.summary
                m.content = d.result || ''
                break
              }
            }
          },
          onNoteUpdated: (d) => {
            this.lastUpdatedNoteId = d.note_id
            this.noteUpdatedFlag++
          },
          onNoteProposal: (d) => {
            // 新提案直接覆盖旧提案（同一时间只需审核最新一份）
            this.pendingProposal = { noteId: d.note_id, tool: d.tool, content: d.content }
          },
          onDone: () => {
            finalizeRound()
            this.fetchConversations() // 更新会话排序/标题
          },
          onError: (d) => {
            errored = true
            const a = ensureAssistant()
            a.streaming = false
            a.thinkStreaming = false
            a.content = a.content ? a.content + `\n\n⚠ ${d.message}` : `⚠ ${d.message}`
          },
        }, ctrl.signal)
      } catch (e: any) {
        if (e?.name === 'AbortError') {
          // 用户主动中断：保留已生成内容，不追加错误提示（后端会把部分输出落库）
          interruptStreamingTools(this.messages)
        } else {
          errored = true
          const a = ensureAssistant()
          a.streaming = false
          a.thinkStreaming = false
          a.content = `⚠ 网络错误：${e.message || '连接中断'}`
        }
      } finally {
        this.sending = false
        if (abortCtrl === ctrl) abortCtrl = null
        finalizeRound()
        // TS 无法跟踪闭包内赋值，这里显式断言
        const last = lastAssistant as ChatMessage | null
        if (errored && last && !last.content) {
          last.content = '⚠ 对话出错，请重试'
        }
      }
    },
  },
})
