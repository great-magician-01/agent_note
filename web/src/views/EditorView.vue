<script setup lang="ts">
import { onMounted, onUnmounted, ref, computed, watch } from 'vue'
import { useRoute, useRouter, onBeforeRouteLeave } from 'vue-router'
import { Editor, Viewer } from '@bytemd/vue-next'
import gfm from '@bytemd/plugin-gfm'
import highlight from '@bytemd/plugin-highlight'
import mermaid from '@bytemd/plugin-mermaid'
import math from '@bytemd/plugin-math'
import 'bytemd/dist/index.css'
import 'katex/dist/katex.css'
import '../styles/bytemd-glass.css'

import TopBar from '../components/TopBar.vue'
import ChatPanel from '../components/ChatPanel.vue'
import DiffReviewModal from '../components/DiffReviewModal.vue'
import GlassDropdown, { type GlassMenuItem } from '../components/GlassDropdown.vue'
import api from '../api/client'
import { useNoteStore, type NoteItem } from '../stores/notes'
import { useCategoryStore } from '../stores/categories'
import { useChatStore } from '../stores/chat'
import { setTitle } from '../composables/useTitle'

const route = useRoute()
const router = useRouter()
const noteStore = useNoteStore()
const catStore = useCategoryStore()
const chat = useChatStore()

const plugins = [gfm(), highlight(), mermaid(), math()]

const noteId = ref<string>((route.params.id as string) || '')
const title = ref('')
const content = ref('')
const categoryId = ref<string | null>(null)
const saving = ref(false)
const savedOnce = ref(false)
const dirty = ref(false)
const error = ref('')
const note = ref<NoteItem | null>(null)
// ByteMD 非受控：接受 AI 提案后自增 key 强制重挂载编辑器
const editorKey = ref(0)
// 窄屏（<lg）写作助手全屏抽屉开关
const mobileChatOpen = ref(false)

// 已有笔记默认预览模式；新建笔记直接进入编辑
const mode = ref<'preview' | 'edit'>(noteId.value ? 'preview' : 'edit')

const isNew = computed(() => !noteId.value)
const isPreview = computed(() => mode.value === 'preview')

// 浏览器标题：新建笔记 / 笔记标题 / 编辑中 · 笔记标题
watch(
  [title, mode, noteId],
  () => {
    if (isNew.value) {
      setTitle('新建笔记')
    } else if (mode.value === 'edit') {
      setTitle(`编辑 · ${title.value || '无标题'}`)
    } else {
      setTitle(title.value || '无标题')
    }
  },
  { immediate: true },
)

const categoryName = computed(() => {
  if (!categoryId.value) return '未分类'
  return catStore.list.find((c) => c.id === categoryId.value)?.name || '未分类'
})

// 分类选择器选项（key '' = 未分类）
const categoryItems = computed<GlassMenuItem[]>(() => [
  { key: '', label: '未分类' },
  ...catStore.list.map((c) => ({ key: c.id, label: c.name })),
])

function onSelectCategory(key: string) {
  categoryId.value = key || null
  dirty.value = true
}

// 删除等低频操作注入顶栏「更多」菜单（新建页无删除）
const moreItems = computed<GlassMenuItem[]>(() =>
  isNew.value ? [] : [{ key: 'delete', label: '删除笔记', danger: true }],
)

function onMoreAction(key: string) {
  if (key === 'delete') removeNote()
}

// 轻提示（统一管理定时器，避免连续触发时旧定时器提前清掉新提示）
const toast = ref('')
let toastTimer: ReturnType<typeof setTimeout> | undefined
function showToast(msg: string) {
  toast.value = msg
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => (toast.value = ''), 3000)
}

// ByteMD 图片上传钩子
async function uploadImages(files: File[]) {
  try {
    const results: { url: string }[] = []
    for (const file of files) {
      const form = new FormData()
      form.append('file', file)
      const { data } = await api.post('/uploads', form, {
        headers: { 'Content-Type': 'multipart/form-data' },
      })
      results.push({ url: data.url })
    }
    return results
  } catch (e: any) {
    showToast(e.response?.data?.error || '图片上传失败，请重试')
    throw e
  }
}

function onChange(v: string) {
  content.value = v
  dirty.value = true
}

async function save(): Promise<boolean> {
  if (saving.value) return false
  saving.value = true
  error.value = ''
  try {
    if (isNew.value) {
      const created = await noteStore.create(content.value, title.value, categoryId.value)
      noteId.value = created.id
      note.value = created
      savedOnce.value = true
      // 替换路由，便于刷新/分享
      router.replace(`/note/${created.id}`)
    } else {
      note.value = await noteStore.update(noteId.value, title.value, content.value, categoryId.value)
    }
    dirty.value = false
    return true
  } catch (e: any) {
    error.value = e.response?.data?.error || '保存失败：网络不可达，修改已保留在编辑器'
    return false
  } finally {
    saving.value = false
  }
}

// 发送消息前自动保存（供 ChatPanel beforeSend 调用）
// 预览模式无未保存内容，直接放行
async function autoSaveBeforeSend(): Promise<boolean> {
  if (isPreview.value) return true
  if (!dirty.value && !isNew.value) return true
  return save()
}

// AI 标题修改（update_note_title 仍直写库）→ 只刷新标题，不动正文/脏标记
async function onNoteUpdated(updatedId: string) {
  if (!noteId.value || updatedId !== noteId.value) {
    showToast('AI 已修改笔记')
    return
  }
  try {
    const n = await noteStore.getOne(noteId.value)
    note.value = n
    title.value = n.title
    showToast('AI 已更新笔记标题')
  } catch {
    // 忽略刷新失败，用户可手动刷新
  }
}

// ---- AI 正文修改提案（note_proposal）→ diff 审核流 ----
const showDiff = ref(false)
const proposal = ref<{ noteId: string; tool: string; content: string } | null>(null)
// 打开弹层时的正文快照，作为 diff 的旧侧
const proposalOldContent = ref('')

// 有待审核提案且属于当前笔记时打开 diff 弹层
function maybeOpenProposal() {
  const p = chat.pendingProposal
  if (!p || p.noteId !== noteId.value) return
  // 笔记内容尚未加载完时先不开，避免 diff 旧侧为空
  if (noteId.value && !savedOnce.value) return
  if (!showDiff.value) proposalOldContent.value = content.value
  proposal.value = p
  showDiff.value = true
}

watch(() => chat.pendingProposal, maybeOpenProposal, { immediate: true })

async function acceptProposal() {
  const p = proposal.value
  if (!p) return
  // AI 运行期间用户又编辑过：接受会覆盖这些编辑，先确认
  if (dirty.value && !confirm('你在 AI 修改期间有新的编辑，接受将覆盖这些修改，确定吗？')) return
  showDiff.value = false
  proposal.value = null
  chat.clearProposal()
  content.value = p.content
  editorKey.value++ // 强制重挂载 ByteMD，展示新正文
  dirty.value = true
  if (await save()) showToast('已应用 AI 修改')
}

function rejectProposal() {
  if (!showDiff.value && !proposal.value) return
  showDiff.value = false
  proposal.value = null
  chat.clearProposal()
  showToast('已拒绝该修改')
}

function enterEdit() {
  mode.value = 'edit'
}

async function enterPreview() {
  // 有未保存内容先保存
  if (dirty.value) {
    const ok = await save()
    if (!ok) return
  }
  mode.value = 'preview'
}

onMounted(async () => {
  if (!catStore.loaded) catStore.fetch()
  if (noteId.value) {
    try {
      const n = await noteStore.getOne(noteId.value)
      note.value = n
      title.value = n.title
      content.value = n.content_md || ''
      categoryId.value = n.category_id
      savedOnce.value = true
      // 加载完成后再检查一次待审核提案（进入页面前提案可能已到达）
      maybeOpenProposal()
    } catch {
      error.value = '笔记不存在或已删除'
    }
  }
})

// 删除笔记
async function removeNote() {
  if (!noteId.value) return
  if (!confirm('删除这篇笔记？关联的 AI 对话也会一并删除。')) return
  await noteStore.remove(noteId.value)
  router.push('/')
}

// Ctrl+S 保存（仅编辑模式）
function onKeydownGlobal(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault()
    if (!isPreview.value) save()
  }
}

// 关闭/刷新页面前的脏保护
function onBeforeUnload(e: BeforeUnloadEvent) {
  if (dirty.value) {
    e.preventDefault()
    e.returnValue = ''
  }
}

// 路由离开：未保存修改先确认；未处理的提案一并清理
onBeforeRouteLeave(() => {
  if (chat.pendingProposal?.noteId === noteId.value) chat.clearProposal()
  if (dirty.value) return confirm('有未保存的修改，确定离开吗？')
})

onMounted(() => {
  window.addEventListener('keydown', onKeydownGlobal)
  window.addEventListener('beforeunload', onBeforeUnload)
})
onUnmounted(() => {
  window.removeEventListener('keydown', onKeydownGlobal)
  window.removeEventListener('beforeunload', onBeforeUnload)
  clearTimeout(toastTimer)
  if (chat.pendingProposal?.noteId === noteId.value) chat.clearProposal()
})
</script>

<template>
  <div class="h-full flex flex-col items-center px-2 lg:px-4">
    <div class="w-full lg:max-w-[70%] flex flex-col h-full py-3 lg:py-6">
      <TopBar :back="true" :more-extra="moreItems" @more-select="onMoreAction">
        <template #title>
          <!-- 预览：静态标题；编辑：输入框 -->
          <span
            v-if="isPreview"
            class="flex-1 min-w-0 text-[18px] font-semibold text-[var(--text-1)] truncate"
          >{{ title || '无标题' }}</span>
          <input
            v-else
            v-model="title"
            type="text"
            placeholder="无标题"
            class="flex-1 min-w-0 bg-transparent text-[18px] font-semibold text-[var(--text-1)] placeholder:text-[var(--text-3)] focus:outline-none border-b border-transparent focus:border-[var(--accent)] transition-colors pb-0.5"
            @input="dirty = true"
          />
        </template>
        <template #actions>
          <!-- 分类：预览显示徽标，编辑显示玻璃下拉选择器 -->
          <span
            v-if="isPreview"
            class="px-2.5 py-1 rounded-full text-[12px] text-[var(--text-2)] border border-[var(--glass-border)]"
          >{{ categoryName }}</span>
          <GlassDropdown
            v-else
            :items="categoryItems"
            :current="categoryId ?? ''"
            align="right"
            panel-class="min-w-[160px] max-h-[320px] overflow-y-auto"
            @select="onSelectCategory"
          >
            <template #trigger="{ open }">
              <button
                type="button"
                class="h-9 pl-3.5 pr-2.5 rounded-[10px] flex items-center gap-1.5 text-[13px] border transition-colors"
                :class="
                  open
                    ? 'bg-[var(--glass-3)] border-[var(--glass-border-strong)] text-[var(--text-1)]'
                    : 'bg-[var(--glass-2)] border-[var(--glass-border)] text-[var(--text-2)] hover:text-[var(--text-1)] hover:border-[var(--glass-border-strong)]'
                "
                title="所属分类"
              >
                <span class="max-w-[140px] truncate">{{ categoryName }}</span>
                <span
                  class="text-[10px] text-[var(--text-3)] transition-transform duration-150"
                  :class="open ? 'rotate-180' : ''"
                >▾</span>
              </button>
            </template>
          </GlassDropdown>

          <span v-if="error" class="text-[12px] text-[var(--danger)] max-w-[300px] truncate">{{ error }}</span>

          <!-- 预览模式：编辑按钮 -->
          <button
            v-if="isPreview"
            class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
            @click="enterEdit"
          >编辑</button>

          <!-- 编辑模式：预览（保存后）+ 保存 -->
          <template v-else>
            <button
              v-if="!isNew"
              class="h-9 px-4 rounded-[10px] text-[13px] text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
              @click="enterPreview"
            >预览</button>
            <button
              class="h-9 px-4 rounded-[10px] text-[13px] font-semibold transition-all"
              :class="
                dirty
                  ? 'text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110'
                  : 'text-[var(--text-3)] bg-transparent border border-[var(--glass-border)]'
              "
              :disabled="saving"
              @click="save"
            >
              {{ saving ? '保存中…' : dirty ? '保存' : '已保存' }}
            </button>
          </template>
        </template>
      </TopBar>

      <main class="glass reveal reveal-2 rounded-[20px] flex-1 mt-3 flex overflow-hidden min-h-0">
        <!-- 预览：ByteMD Viewer -->
        <section v-if="isPreview" class="flex-1 min-w-0 note-preview">
          <Viewer :value="content" :plugins="plugins" />
        </section>

        <!-- 编辑：ByteMD 编辑器（key 变化强制重挂载，用于应用 AI 提案） -->
        <section v-else class="flex-1 min-w-0 bytemd-host">
          <Editor
            :key="editorKey"
            :value="content"
            :plugins="plugins"
            :upload-images="uploadImages"
            placeholder="开始书写… 支持 Markdown、粘贴图片自动上传"
            @change="onChange"
          />
        </section>

        <!-- 右：写作助手（窄屏为全屏抽屉，transform 滑入滑出） -->
        <aside
          class="w-[380px] shrink-0 border-l border-[var(--glass-border)] flex flex-col min-h-0 max-lg:fixed max-lg:inset-0 max-lg:z-40 max-lg:w-full max-lg:border-l-0 max-lg:bg-[var(--ink)] max-lg:transition-transform max-lg:duration-300"
          :class="mobileChatOpen ? 'max-lg:translate-x-0' : 'max-lg:translate-x-full'"
        >
          <div class="lg:hidden shrink-0 flex items-center justify-between px-4 py-2.5 border-b border-[var(--glass-border)]">
            <span class="text-[13px] text-[var(--text-2)]">写作助手</span>
            <button
              class="w-8 h-8 rounded-[8px] flex items-center justify-center text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
              title="关闭"
              @click="mobileChatOpen = false"
            >✕</button>
          </div>
          <div class="flex-1 min-h-0 flex flex-col">
            <ChatPanel
              scope="note"
              :note-id="noteId"
              :note-title="title"
              :before-send="autoSaveBeforeSend"
              @note-updated="onNoteUpdated"
            />
          </div>
        </aside>
      </main>
    </div>

    <!-- 窄屏：打开写作助手抽屉的浮钮 -->
    <button
      v-if="!mobileChatOpen"
      class="lg:hidden fixed bottom-5 right-5 z-30 w-12 h-12 rounded-full text-white text-[18px] bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] shadow-lg hover:brightness-110 transition-all"
      title="写作助手"
      @click="mobileChatOpen = true"
    >✦</button>

    <!-- 轻提示 -->
    <Transition name="fade">
      <div
        v-if="toast"
        class="fixed bottom-6 right-6 z-50 glass-3 rounded-[10px] px-4 py-2.5 text-[13px] text-[var(--text-1)] flex items-center gap-2"
      >
        <span class="w-2 h-2 rounded-full bg-[var(--accent)]" />
        {{ toast }}
      </div>
    </Transition>

    <!-- AI 正文修改提案的 diff 审核弹层 -->
    <DiffReviewModal
      v-if="showDiff && proposal"
      :old-content="proposalOldContent"
      :new-content="proposal.content"
      :tool="proposal.tool"
      @accept="acceptProposal"
      @reject="rejectProposal"
      @close="rejectProposal"
    />
  </div>
</template>
