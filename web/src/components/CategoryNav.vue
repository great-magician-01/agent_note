<script setup lang="ts">
import { ref } from 'vue'
import { useCategoryStore } from '../stores/categories'

const catStore = useCategoryStore()

const showModal = ref(false)
const newName = ref('')
const editingId = ref('')
const error = ref('')

function openCreate() {
  editingId.value = ''
  newName.value = ''
  error.value = ''
  showModal.value = true
}

function openRename(id: string, name: string) {
  editingId.value = id
  newName.value = name
  error.value = ''
  showModal.value = true
}

async function submit() {
  const name = newName.value.trim()
  if (!name) return
  try {
    if (editingId.value) {
      await catStore.rename(editingId.value, name)
    } else {
      await catStore.create(name)
    }
    showModal.value = false
  } catch (e: any) {
    error.value = e.response?.data?.error || '操作失败'
  }
}

async function remove(id: string, name: string) {
  if (!confirm(`删除分类「${name}」？其下笔记将变为未分类。`)) return
  await catStore.remove(id)
}

const itemClass = (active: boolean) =>
  [
    'group flex items-center h-10 px-3 rounded-[10px] cursor-pointer transition-colors text-[14px]',
    active
      ? 'bg-[var(--glass-3)] text-[var(--text-1)] border-l-4 border-l-[var(--accent)] pl-2'
      : 'text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)]',
  ].join(' ')
</script>

<template>
  <nav class="w-[220px] shrink-0 flex flex-col py-4 pl-4 pr-3 overflow-y-auto">
    <div class="text-[12px] tracking-[1px] text-[var(--text-3)] px-3 mb-2">分类</div>

    <!-- 全部笔记 -->
    <div :class="itemClass(catStore.activeId === '')" @click="catStore.select('')">
      <span class="flex-1 truncate">全部笔记</span>
    </div>

    <!-- 未分类 -->
    <div :class="itemClass(catStore.activeId === '0')" @click="catStore.select('0')">
      <span class="flex-1 truncate">未分类</span>
    </div>

    <!-- 分类列表 -->
    <div
      v-for="cat in catStore.list"
      :key="cat.id"
      :class="itemClass(catStore.activeId === cat.id)"
      @click="catStore.select(cat.id)"
    >
      <span class="flex-1 truncate">{{ cat.name }}</span>
      <span class="text-[12px] text-[var(--text-3)] tabular-nums mr-1">{{ cat.note_count }}</span>
      <span class="hidden group-hover:flex items-center gap-1">
        <button
          class="text-[12px] text-[var(--text-3)] hover:text-[var(--text-1)]"
          title="重命名"
          @click.stop="openRename(cat.id, cat.name)"
        >✎</button>
        <button
          class="text-[12px] text-[var(--text-3)] hover:text-[var(--danger)]"
          title="删除"
          @click.stop="remove(cat.id, cat.name)"
        >✕</button>
      </span>
    </div>

    <!-- 新建分类 -->
    <button
      class="mt-2 h-9 px-3 rounded-[10px] text-left text-[13px] text-[var(--text-3)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
      @click="openCreate"
    >
      ＋ 新建分类
    </button>

    <!-- 弹窗 -->
    <Teleport to="body">
      <div
        v-if="showModal"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-[8px]"
        @click.self="showModal = false"
      >
        <div class="glass-2 rounded-[20px] w-[360px] p-6 reveal">
          <h3 class="text-[15px] font-semibold mb-4">{{ editingId ? '重命名分类' : '新建分类' }}</h3>
          <input
            v-model="newName"
            type="text"
            placeholder="分类名称"
            class="w-full h-10 px-3 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[var(--text-1)] placeholder:text-[var(--text-3)] focus:border-[var(--glass-border-strong)] focus:outline-none"
            @keyup.enter="submit"
          />
          <p v-if="error" class="mt-2 text-[12px] text-[var(--danger)]">{{ error }}</p>
          <div class="mt-5 flex justify-end gap-2">
            <button
              class="h-9 px-4 rounded-[10px] text-[13px] text-[var(--text-2)] hover:bg-[var(--glass-2)] transition-colors"
              @click="showModal = false"
            >取消</button>
            <button
              class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all disabled:opacity-50"
              :disabled="!newName.trim()"
              @click="submit"
            >保存</button>
          </div>
        </div>
      </div>
    </Teleport>
  </nav>
</template>
