<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import TopBar from '../components/TopBar.vue'
import CategoryNav from '../components/CategoryNav.vue'
import NoteCard from '../components/NoteCard.vue'
import ChatPanel from '../components/ChatPanel.vue'
import GlassDropdown, { type GlassMenuItem } from '../components/GlassDropdown.vue'
import { useCategoryStore } from '../stores/categories'
import { useNoteStore } from '../stores/notes'
import { setTitle } from '../composables/useTitle'

const router = useRouter()
const catStore = useCategoryStore()
const noteStore = useNoteStore()

let debounceTimer: ReturnType<typeof setTimeout> | undefined
let metaPollTimer: ReturnType<typeof setInterval> | undefined

function onSearch(kw: string) {
  clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    noteStore.setKeyword(kw)
    noteStore.fetch(catStore.activeId || undefined)
  }, 300)
}

function refresh() {
  noteStore.fetch(catStore.activeId || undefined)
}

async function onRetryMeta(id: string) {
  await noteStore.regenerateMeta(id)
  refresh()
}

// 实体芯片点击 → 按该实体过滤
function onFilterEntity(name: string) {
  noteStore.setEntityFilter(name)
  refresh()
}

function clearEntityFilter() {
  noteStore.setEntityFilter('')
  refresh()
}

// ---- 多选模式 ----
const selectMode = ref(false)
const selected = ref<Set<string>>(new Set())

function toggleSelect(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function onCardClick(id: string) {
  if (selectMode.value) {
    toggleSelect(id)
  } else {
    router.push(`/note/${id}`)
  }
}

const allSelected = computed(
  () => noteStore.items.length > 0 && noteStore.items.every((n) => selected.value.has(n.id)),
)

function toggleSelectAll() {
  if (allSelected.value) {
    selected.value = new Set()
  } else {
    selected.value = new Set(noteStore.items.map((n) => n.id))
  }
}

function exitSelectMode() {
  selectMode.value = false
  selected.value = new Set()
}

// 批量移动到分类（key '' = 未分类）
const moveItems = computed<GlassMenuItem[]>(() => [
  { key: '', label: '未分类' },
  ...catStore.list.map((c) => ({ key: c.id, label: c.name })),
])

const batchBusy = ref(false)

async function onBatchMove(key: string) {
  if (selected.value.size === 0 || batchBusy.value) return
  batchBusy.value = true
  try {
    await noteStore.batchMove([...selected.value], key || null)
    exitSelectMode()
    refresh()
  } finally {
    batchBusy.value = false
  }
}

async function onBatchDelete() {
  const n = selected.value.size
  if (n === 0 || batchBusy.value) return
  if (!confirm(`删除选中的 ${n} 篇笔记？关联的 AI 对话也会一并删除。`)) return
  batchBusy.value = true
  try {
    await noteStore.batchRemove([...selected.value])
    exitSelectMode()
  } finally {
    batchBusy.value = false
  }
}

// 全局快捷键：Ctrl+N 新建笔记，Ctrl+K 聚焦搜索
function onGlobalKeydown(e: KeyboardEvent) {
  if (!(e.ctrlKey || e.metaKey)) return
  if (e.key === 'n') {
    e.preventDefault()
    router.push('/note/new')
  } else if (e.key === 'k') {
    e.preventDefault()
    document.getElementById('global-search')?.focus()
  }
}

// 有笔记处于 pending/processing 时轮询刷新，元数据生成完成自动出现
function ensureMetaPolling() {
  clearInterval(metaPollTimer)
  metaPollTimer = setInterval(() => {
    const hasPending = noteStore.items.some(
      (n) => n.meta_status === 'pending' || n.meta_status === 'processing',
    )
    if (hasPending) refresh()
  }, 5000)
}

watch(
  () => catStore.activeId,
  () => {
    exitSelectMode()
    refresh()
  },
)

// 浏览器标题跟随当前分类视图
watch(
  [() => catStore.activeId, () => catStore.list],
  () => {
    const name =
      catStore.activeId === ''
        ? '全部笔记'
        : catStore.activeId === '0'
          ? '未分类'
          : catStore.list.find((c) => c.id === catStore.activeId)?.name
    setTitle(name || undefined)
  },
  { immediate: true },
)

onMounted(async () => {
  await catStore.fetch()
  refresh()
  ensureMetaPolling()
  window.addEventListener('keydown', onGlobalKeydown)
})

onUnmounted(() => {
  clearTimeout(debounceTimer)
  clearInterval(metaPollTimer)
  window.removeEventListener('keydown', onGlobalKeydown)
})
</script>

<template>
  <div class="h-full flex flex-col items-center px-4">
    <div class="w-full max-w-[70%] flex flex-col h-full py-6">
      <TopBar :show-search="true" @search="onSearch" />

      <main class="glass reveal reveal-2 rounded-[20px] flex-1 mt-3 flex overflow-hidden min-h-0">
        <!-- 左：分类导航 -->
        <CategoryNav />

        <!-- 中：笔记列表 -->
        <section class="flex-1 min-w-0 flex flex-col border-l border-[var(--glass-border)]">
          <div class="px-5 pt-4 pb-3 flex items-baseline gap-3 shrink-0">
            <h2 class="text-[15px] font-semibold">
              {{ catStore.activeId === '' ? '全部笔记' : catStore.activeId === '0' ? '未分类' : catStore.list.find(c => c.id === catStore.activeId)?.name || '笔记' }}
            </h2>
            <span class="text-[12px] text-[var(--text-3)] tabular-nums">{{ noteStore.total }} 篇</span>
            <!-- 实体过滤指示 -->
            <span
              v-if="noteStore.entityFilter"
              class="flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[12px] text-[var(--accent)] border border-[color-mix(in_srgb,var(--accent)_40%,transparent)]"
            >
              实体：{{ noteStore.entityFilter }}
              <button class="hover:text-[var(--danger)]" title="清除过滤" @click="clearEntityFilter">✕</button>
            </span>
            <span class="flex-1" />
            <!-- 多选开关 -->
            <button
              v-if="noteStore.items.length > 0"
              class="h-7 px-3 rounded-[8px] text-[12px] transition-colors"
              :class="
                selectMode
                  ? 'text-[var(--accent)] border border-[color-mix(in_srgb,var(--accent)_45%,transparent)]'
                  : 'text-[var(--text-3)] border border-transparent hover:text-[var(--text-1)] hover:bg-[var(--glass-2)]'
              "
              @click="selectMode ? exitSelectMode() : (selectMode = true)"
            >{{ selectMode ? '完成' : '多选' }}</button>
          </div>

          <div class="flex-1 overflow-y-auto px-5 pb-5 space-y-3">
            <NoteCard
              v-for="note in noteStore.items"
              :key="note.id"
              :note="note"
              :selectable="selectMode"
              :selected="selected.has(note.id)"
              @click="onCardClick(note.id)"
              @retry-meta="onRetryMeta"
              @filter-entity="onFilterEntity"
            />

            <!-- 空状态 -->
            <div
              v-if="!noteStore.loading && noteStore.items.length === 0"
              class="h-full flex flex-col items-center justify-center gap-3 py-20"
            >
              <div class="w-20 h-20 rounded-full border border-dashed border-[var(--glass-border-strong)] flex items-center justify-center text-[28px] text-[var(--text-3)]">
                ✎
              </div>
              <p class="text-[var(--text-2)] text-[14px]">
                {{ noteStore.keyword ? '没有匹配的笔记' : '还没有笔记' }}
              </p>
              <button
                v-if="!noteStore.keyword"
                class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
                @click="router.push('/note/new')"
              >写第一篇</button>
            </div>
          </div>

          <!-- 多选操作条 -->
          <div
            v-if="selectMode"
            class="shrink-0 border-t border-[var(--glass-border)] px-5 py-3 flex items-center gap-3"
          >
            <button
              class="flex items-center gap-2 text-[13px] text-[var(--text-2)] hover:text-[var(--text-1)] transition-colors"
              @click="toggleSelectAll"
            >
              <span
                class="w-[16px] h-[16px] rounded-[5px] border flex items-center justify-center text-[10px] transition-colors"
                :class="
                  allSelected
                    ? 'bg-[var(--accent)] border-[var(--accent)] text-white'
                    : 'border-[var(--glass-border-strong)] text-transparent'
                "
              >✓</span>
              全选
            </button>
            <span class="text-[12px] text-[var(--text-3)] tabular-nums">已选 {{ selected.size }} 篇</span>
            <span class="flex-1" />

            <GlassDropdown
              :items="moveItems"
              align="right"
              placement="top"
              panel-class="min-w-[160px] max-h-[300px] overflow-y-auto"
              @select="onBatchMove"
            >
              <template #trigger>
                <button
                  type="button"
                  class="h-8 px-3.5 rounded-[9px] text-[13px] text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors disabled:opacity-40 disabled:pointer-events-none"
                  :disabled="selected.size === 0 || batchBusy"
                >移动到…</button>
              </template>
            </GlassDropdown>
            <button
              class="h-8 px-3.5 rounded-[9px] text-[13px] text-[var(--danger)] border border-[color-mix(in_srgb,var(--danger)_35%,transparent)] hover:bg-[color-mix(in_srgb,var(--danger)_12%,transparent)] transition-colors disabled:opacity-40 disabled:pointer-events-none"
              :disabled="selected.size === 0 || batchBusy"
              @click="onBatchDelete"
            >删除</button>
          </div>
        </section>

        <!-- 右：AI 对话栏 -->
        <aside class="w-[380px] shrink-0 border-l border-[var(--glass-border)] flex flex-col min-h-0">
          <ChatPanel scope="global" />
        </aside>
      </main>
    </div>
  </div>
</template>
