<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import GlassDropdown, { type GlassMenuItem } from './GlassDropdown.vue'

const props = withDefaults(
  defineProps<{
    showSearch?: boolean
    back?: boolean
    title?: string
    /** 追加到「更多」菜单顶部的页面级操作（如删除笔记） */
    moreExtra?: GlassMenuItem[]
  }>(),
  { moreExtra: () => [] },
)

const emit = defineEmits<{
  search: [kw: string]
  /** 点击了 moreExtra 里的项（logout 由 TopBar 自己处理） */
  moreSelect: [key: string]
}>()

const router = useRouter()
const auth = useAuthStore()

function onSearchInput(e: Event) {
  emit('search', (e.target as HTMLInputElement).value)
}

// 页面级操作在上，退出登录固定垫底；有额外项时加分隔线
const moreItems = computed<GlassMenuItem[]>(() => [
  ...props.moreExtra,
  { key: 'logout', label: '退出登录', danger: true, divider: props.moreExtra.length > 0 },
])

function onMore(key: string) {
  if (key === 'logout') {
    auth.logout()
    router.push('/login')
    return
  }
  emit('moreSelect', key)
}
</script>

<template>
  <header class="glass reveal reveal-1 rounded-[20px] h-[60px] flex items-center gap-3 px-5 shrink-0 relative z-30">
    <!-- 返回 -->
    <button
      v-if="back"
      class="w-9 h-9 rounded-full flex items-center justify-center text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
      title="返回"
      @click="router.back()"
    >
      ←
    </button>

    <!-- Logo -->
    <div
      v-if="!back"
      class="text-[18px] font-bold bg-gradient-to-r from-[var(--aurora-teal)] to-[var(--aurora-violet)] bg-clip-text text-transparent cursor-pointer select-none"
      @click="router.push('/')"
    >
      ◈ Note
    </div>

    <!-- 标题（编辑页） -->
    <slot name="title" />

    <!-- 搜索框 -->
    <div v-if="showSearch" class="flex-1 flex justify-center">
      <div class="relative w-[360px] max-w-full">
        <span class="absolute left-3.5 top-1/2 -translate-y-1/2 text-[var(--text-3)] text-[13px]">⌕</span>
        <input
          id="global-search"
          type="text"
          placeholder="搜索笔记（标题 / 正文）…"
          class="w-full h-9 pl-9 pr-4 rounded-full bg-[var(--glass-2)] border border-[var(--glass-border)] text-[13px] text-[var(--text-1)] placeholder:text-[var(--text-3)] focus:border-[var(--glass-border-strong)] focus:shadow-[0_0_0_3px_rgba(45,212,191,.15)] focus:outline-none transition-all"
          @input="onSearchInput"
        />
      </div>
    </div>
    <div v-else class="flex-1" />

    <!-- 右侧按钮 -->
    <slot name="actions" />
    <button
      v-if="showSearch"
      class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
      @click="router.push('/note/new')"
    >
      ＋ 新增笔记
    </button>
    <button
      class="w-9 h-9 rounded-full flex items-center justify-center text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
      title="设置"
      @click="router.push('/settings')"
    >
      ⚙
    </button>
    <GlassDropdown :items="moreItems" @select="onMore" />
  </header>
</template>
