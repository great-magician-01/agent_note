<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import MarkdownIt from 'markdown-it'
import { buildViewItems, toolLabel, useChatStore, type ChatViewItem } from '../stores/chat'
import { useAIConfigStore } from '../stores/aiConfigs'

const props = defineProps<{
  scope: 'global' | 'note'
  noteId?: string
  noteTitle?: string
  // 编辑页传入：发消息前自动保存当前编辑器内容
  beforeSend?: () => Promise<boolean>
}>()

const emit = defineEmits<{
  noteUpdated: [noteId: string]
}>()

const router = useRouter()
const chat = useChatStore()
const aiConfigs = useAIConfigStore()

const md = new MarkdownIt({ linkify: true, breaks: true })

const input = ref('')
const listEl = ref<HTMLElement>()
const showDrawer = ref(false)

// 消息列表 → 视图项（思考 / 正文 / 工具调用，按模型输出顺序）
const viewItems = computed(() => buildViewItems(chat.messages))

// 展开状态：思考/工具块默认折叠；思考流式期间自动展开
const expandedKeys = ref(new Set<string>())
function isExpanded(key: string, streaming?: boolean): boolean {
  return !!streaming || expandedKeys.value.has(key)
}
function toggleExpand(key: string) {
  const s = new Set(expandedKeys.value)
  if (s.has(key)) s.delete(key)
  else s.add(key)
  expandedKeys.value = s
}

// ---- markdown 渲染缓存 + 流式节流（~80ms）----
// 流式期间每个 delta 都会触发重渲，逐个 md.render 太贵：
// 缓存每条消息的渲染结果，streaming 时限频刷新，结束后（streaming=false）立即终渲
const mdCache = new Map<string, { src: string; html: string; at: number }>()
const renderTick = ref(0)
let mdTimer: ReturnType<typeof setTimeout> | undefined

function renderMd(item: ChatViewItem): string {
  renderTick.value // 依赖收集：节流定时器到点后 bump，触发重渲
  const cached = mdCache.get(item.key)
  if (cached && cached.src === item.content) return cached.html
  if (item.streaming && cached) {
    const elapsed = Date.now() - cached.at
    if (elapsed < 80) {
      if (!mdTimer) {
        mdTimer = setTimeout(() => {
          mdTimer = undefined
          renderTick.value++
        }, 80 - elapsed + 5)
      }
      return cached.html
    }
  }
  const html = md.render(item.content)
  mdCache.set(item.key, { src: item.content, html, at: Date.now() })
  return html
}

// 切换会话时清掉渲染缓存
watch(
  () => chat.currentId,
  () => mdCache.clear(),
)

// 工具结果/参数展示：尝试 JSON 美化
function prettyText(raw?: string): string {
  if (!raw) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

// ---- 滚动：用户原本在底部（距底 <60px）才吸底，否则显示「回到底部」浮钮 ----
const stuckToBottom = ref(true)
const showBackToBottom = ref(false)

function isNearBottom(): boolean {
  const el = listEl.value
  if (!el) return true
  return el.scrollHeight - el.scrollTop - el.clientHeight < 60
}

function onScroll() {
  stuckToBottom.value = isNearBottom()
  if (stuckToBottom.value) showBackToBottom.value = false
}

async function scrollToBottom() {
  await nextTick()
  stuckToBottom.value = true
  showBackToBottom.value = false
  if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
}

watch(
  () => {
    const items = viewItems.value
    const last = items[items.length - 1]
    return `${items.length}|${last?.content.length ?? 0}`
  },
  async () => {
    if (stuckToBottom.value) {
      await scrollToBottom()
    } else {
      showBackToBottom.value = true
    }
  },
)

// 写作工具改库 → 通知父组件刷新编辑器
watch(
  () => chat.noteUpdatedFlag,
  () => {
    if (chat.lastUpdatedNoteId) emit('noteUpdated', chat.lastUpdatedNoteId)
  },
)

async function send() {
  const text = input.value.trim()
  if (!text) return
  // 编辑页：发送前自动保存，保证 AI 基于最新内容操作
  if (props.beforeSend) {
    const ok = await props.beforeSend()
    if (!ok) return // 保存失败则不发送（编辑器里已有错误提示）
  }
  input.value = ''
  await chat.send(text)
  scrollToBottom()
}

function onKeydown(e: KeyboardEvent) {
  // IME 输入法组词期间的 Enter 不发送
  if (e.isComposing) return
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

onMounted(async () => {
  if (!aiConfigs.loaded) await aiConfigs.fetch()
  await chat.switchScope(props.scope === 'note' ? props.noteId || '' : '')
  scrollToBottom()
})

// 编辑页 noteId 变化时（新建→已保存）切换作用域
watch(
  () => props.noteId,
  async (id) => {
    if (props.scope === 'note' && id && chat.scopeNoteId !== id) {
      await chat.switchScope(id)
    }
  },
)

// 离开页面时中断在飞消息（后端随连接断开取消上游请求）
onUnmounted(() => {
  clearTimeout(mdTimer)
  chat.stop()
})
</script>

<template>
  <div class="flex flex-col h-full min-h-0 relative">
    <!-- 头部 -->
    <div class="px-4 py-3 border-b border-[var(--glass-border)] shrink-0 flex items-center gap-2">
      <div class="flex-1 min-w-0">
        <h3 class="text-[14px] font-semibold">{{ scope === 'note' ? '写作助手' : 'AI 助手' }}</h3>
        <p v-if="scope === 'note' && noteTitle" class="text-[12px] text-[var(--text-3)] truncate mt-0.5">
          {{ noteTitle }}
        </p>
      </div>
      <button
        class="w-8 h-8 rounded-[8px] flex items-center justify-center text-[13px] text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
        title="会话列表"
        @click="showDrawer = !showDrawer"
      >☰</button>
      <button
        class="w-8 h-8 rounded-[8px] flex items-center justify-center text-[13px] text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
        title="新对话"
        @click="chat.newConversation()"
      >＋</button>
    </div>

    <!-- 会话抽屉 -->
    <Transition name="fade">
      <div
        v-if="showDrawer"
        class="absolute inset-x-0 top-[53px] bottom-0 z-20 glass-3 flex flex-col"
      >
        <div class="px-4 py-2 text-[12px] text-[var(--text-3)] border-b border-[var(--glass-border)]">
          会话列表
        </div>
        <div class="flex-1 overflow-y-auto p-2 space-y-1">
          <div
            v-for="conv in chat.conversations"
            :key="conv.id"
            class="group flex items-center gap-2 px-3 h-10 rounded-[10px] cursor-pointer text-[13px] transition-colors"
            :class="conv.id === chat.currentId
              ? 'bg-[var(--glass-3)] text-[var(--text-1)]'
              : 'text-[var(--text-2)] hover:bg-[var(--glass-2)]'"
            @click="chat.select(conv.id); showDrawer = false"
          >
            <span class="flex-1 truncate">{{ conv.title }}</span>
            <button
              class="hidden group-hover:block text-[var(--text-3)] hover:text-[var(--danger)]"
              title="删除会话"
              @click.stop="chat.removeConversation(conv.id)"
            >✕</button>
          </div>
          <p v-if="chat.conversations.length === 0" class="text-center text-[12px] text-[var(--text-3)] py-8">
            暂无历史会话
          </p>
        </div>
      </div>
    </Transition>

    <!-- 无 AI 配置提示 -->
    <div
      v-if="aiConfigs.loaded && !aiConfigs.activeConfig"
      class="flex-1 flex flex-col items-center justify-center gap-3 px-6 text-center"
    >
      <div class="w-16 h-16 rounded-full border border-dashed border-[var(--glass-border-strong)] flex items-center justify-center text-[22px] text-[var(--text-3)]">
        ⚙
      </div>
      <p class="text-[13px] text-[var(--text-2)]">先在设置中添加并激活 AI 配置</p>
      <button
        class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
        @click="router.push('/settings')"
      >去设置</button>
    </div>

    <template v-else>
      <!-- 消息区 -->
      <div ref="listEl" class="flex-1 overflow-y-auto px-4 py-4 space-y-4 min-h-0" @scroll="onScroll">
        <!-- 空状态 -->
        <div
          v-if="chat.messages.length === 0"
          class="h-full flex flex-col items-center justify-center gap-4"
        >
          <p class="text-[13px] text-[var(--text-2)]">问我点什么</p>
          <div class="flex flex-col gap-2 w-full max-w-[260px]">
            <button
              v-for="q in scope === 'note'
                ? ['帮我把这篇文章写下去', '润色一下这篇笔记', '给这篇笔记起个更好的标题']
                : ['我最近的笔记都写了什么', '帮我找关于某个主题的笔记', '总结我所有笔记的主题']"
              :key="q"
              class="px-3 py-2 rounded-[10px] text-[12px] text-[var(--text-2)] bg-[var(--glass-2)] border border-[var(--glass-border)] hover:border-[var(--glass-border-strong)] hover:text-[var(--text-1)] transition-colors text-left"
              @click="input = q; send()"
            >{{ q }}</button>
          </div>
        </div>

        <template v-for="item in viewItems" :key="item.key">
          <!-- 用户气泡 -->
          <div v-if="item.kind === 'user'" class="flex justify-end">
            <div
              class="max-w-[85%] px-3.5 py-2.5 rounded-[14px] rounded-br-[4px] text-[13px] leading-relaxed text-white bg-gradient-to-br from-[var(--user-blue)] to-[var(--user-blue-deep)] whitespace-pre-wrap break-words"
            >{{ item.content }}</div>
          </div>

          <!-- 思考块（默认折叠，流式期间自动展开） -->
          <div v-else-if="item.kind === 'think'" class="rounded-[10px] border border-[var(--glass-border)] bg-[var(--glass-1)] overflow-hidden">
            <button
              type="button"
              class="w-full flex items-center gap-2 px-3 py-2 text-[12px] text-[var(--text-2)] hover:text-[var(--text-1)] transition-colors"
              @click="toggleExpand(item.key)"
            >
              <span v-if="item.streaming" class="pulse-dot" />
              <span v-else class="text-[var(--aurora-violet)]">✦</span>
              <span class="font-medium">思考过程</span>
              <span
                class="ml-auto text-[10px] text-[var(--text-3)] transition-transform duration-150"
                :class="isExpanded(item.key, item.streaming) ? 'rotate-180' : ''"
              >▾</span>
            </button>
            <div
              v-if="isExpanded(item.key, item.streaming)"
              class="px-3 pb-2.5 text-[12px] leading-relaxed text-[var(--text-3)] whitespace-pre-wrap break-words max-h-[240px] overflow-y-auto think-body"
            >{{ item.content }}<span v-if="item.streaming" class="inline-block w-1.5 h-3.5 ml-0.5 align-middle bg-[var(--aurora-violet)] animate-pulse" /></div>
          </div>

          <!-- 工具调用块（默认折叠，可展开看参数/结果） -->
          <div v-else-if="item.kind === 'tool'" class="rounded-[10px] border border-[var(--glass-border)] bg-[var(--glass-1)] overflow-hidden">
            <button
              type="button"
              class="w-full flex items-center gap-2 px-3 py-2 text-[12px] transition-colors"
              @click="toggleExpand(item.key)"
            >
              <span v-if="item.streaming" class="pulse-dot" />
              <span
                v-else
                class="w-2 h-2 rounded-full shrink-0"
                :class="item.ok ? 'bg-[var(--success)]' : 'bg-[var(--danger)]'"
              />
              <span class="font-medium text-[var(--text-2)]">🔧 {{ toolLabel(item.name) }}</span>
              <span class="truncate text-[var(--text-3)]" :class="{ 'text-[var(--danger)]': item.ok === false }">{{ item.summary }}</span>
              <span
                class="ml-auto text-[10px] text-[var(--text-3)] transition-transform duration-150 shrink-0"
                :class="isExpanded(item.key) ? 'rotate-180' : ''"
              >▾</span>
            </button>
            <div v-if="isExpanded(item.key)" class="px-3 pb-2.5 space-y-2">
              <div v-if="item.input">
                <div class="text-[11px] text-[var(--text-3)] mb-1">参数</div>
                <pre class="tool-detail">{{ item.input }}</pre>
              </div>
              <div v-if="item.result">
                <div class="text-[11px] text-[var(--text-3)] mb-1">结果</div>
                <pre class="tool-detail">{{ prettyText(item.result) }}</pre>
              </div>
            </div>
          </div>

          <!-- AI 正文气泡 -->
          <div v-else class="flex justify-start">
            <div
              class="max-w-[92%] px-3.5 py-2.5 rounded-[14px] rounded-bl-[4px] glass-2 text-[13px] leading-relaxed break-words chat-md"
            >
              <div v-if="item.content" v-html="renderMd(item)" />
              <span v-if="item.streaming" class="inline-block w-2 h-4 ml-0.5 align-middle bg-[var(--accent)] animate-pulse" />
            </div>
          </div>
        </template>
      </div>

      <!-- 「回到底部」浮钮：用户上翻时有新内容流入才显示（定位在滚动容器外） -->
      <Transition name="fade">
        <button
          v-if="showBackToBottom"
          class="absolute bottom-[76px] left-1/2 -translate-x-1/2 z-10 h-8 px-3.5 rounded-full glass-3 text-[12px] text-[var(--text-1)] hover:bg-[var(--glass-border-strong)] transition-colors flex items-center gap-1.5"
          @click="scrollToBottom"
        >
          <span class="text-[10px]">↓</span> 回到底部
        </button>
      </Transition>

      <!-- 输入区 -->
      <div class="p-3 border-t border-[var(--glass-border)] shrink-0">
        <div class="flex items-end gap-2">
          <textarea
            v-model="input"
            rows="1"
            placeholder="输入消息，Enter 发送 / Shift+Enter 换行"
            class="flex-1 max-h-[160px] px-3.5 py-2.5 rounded-[14px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[13px] text-[var(--text-1)] placeholder:text-[var(--text-3)] focus:border-[var(--glass-border-strong)] focus:outline-none resize-none transition-colors"
            @keydown="onKeydown"
          />
          <button
            v-if="chat.sending"
            class="w-9 h-9 rounded-full flex items-center justify-center text-[var(--danger)] bg-[var(--glass-3)] hover:bg-[var(--glass-border-strong)] transition-all shrink-0"
            title="停止生成"
            @click="chat.stop()"
          >
            <span class="text-[11px]">■</span>
          </button>
          <button
            v-else
            class="w-9 h-9 rounded-full flex items-center justify-center text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed transition-all shrink-0"
            :disabled="!input.trim()"
            title="发送"
            @click="send"
          >
            <span>↑</span>
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.chat-md :deep(p) { margin: 0.4em 0; }
.chat-md :deep(p:first-child) { margin-top: 0; }
.chat-md :deep(p:last-child) { margin-bottom: 0; }
.chat-md :deep(pre) {
  background: rgba(0, 0, 0, 0.35);
  border: 1px solid var(--glass-border);
  border-radius: 8px;
  padding: 10px 12px;
  overflow-x: auto;
  margin: 0.5em 0;
}
.chat-md :deep(code) {
  font-family: var(--font-mono);
  font-size: 12px;
  background: var(--glass-2);
  border-radius: 4px;
  padding: 1px 5px;
}
.chat-md :deep(pre code) { background: transparent; padding: 0; }
.chat-md :deep(ul), .chat-md :deep(ol) { padding-left: 1.4em; margin: 0.4em 0; }
.chat-md :deep(a) { color: var(--accent); text-decoration: underline; }
.chat-md :deep(h1), .chat-md :deep(h2), .chat-md :deep(h3) {
  font-size: 14px; font-weight: 600; margin: 0.6em 0 0.3em;
}
.chat-md :deep(blockquote) {
  border-left: 2px solid var(--accent);
  padding-left: 10px;
  color: var(--text-2);
  margin: 0.4em 0;
}
/* 工具调用展开详情 */
.tool-detail {
  font-family: var(--font-mono);
  font-size: 11px;
  line-height: 1.55;
  color: var(--text-2);
  background: rgba(0, 0, 0, 0.3);
  border: 1px solid var(--glass-border);
  border-radius: 8px;
  padding: 8px 10px;
  max-height: 200px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.think-body::-webkit-scrollbar,
.tool-detail::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}
</style>
