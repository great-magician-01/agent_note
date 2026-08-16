<script setup lang="ts">
import type { NoteItem } from '../stores/notes'

withDefaults(
  defineProps<{
    note: NoteItem
    selectable?: boolean
    selected?: boolean
  }>(),
  { selectable: false, selected: false },
)

const emit = defineEmits<{
  filterEntity: [name: string]
  retryMeta: [id: string]
}>()

// 相对时间
function relTime(iso: string): string {
  const t = new Date(iso).getTime()
  const diff = Date.now() - t
  const m = Math.floor(diff / 60000)
  if (m < 1) return '刚刚'
  if (m < 60) return `${m} 分钟前`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h} 小时前`
  const d = Math.floor(h / 24)
  if (d < 30) return `${d} 天前`
  return new Date(iso).toLocaleDateString('zh-CN')
}

const entityColor = (type: string) => `var(--entity-${type}, var(--entity-other))`
</script>

<template>
  <article
    class="glass-2 rounded-[14px] p-4 cursor-pointer transition-all duration-150 hover:bg-[var(--glass-3)] hover:border-[var(--glass-border-strong)]"
    :class="[
      selected
        ? 'border-[var(--accent)] shadow-[0_0_0_1px_var(--accent),0_8px_24px_-12px_var(--accent)]'
        : 'hover:-translate-y-0.5',
    ]"
  >
    <div class="flex items-start gap-2">
      <!-- 多选复选框 -->
      <span
        v-if="selectable"
        class="mt-1 w-[18px] h-[18px] rounded-[6px] border flex items-center justify-center shrink-0 text-[11px] transition-colors"
        :class="
          selected
            ? 'bg-[var(--accent)] border-[var(--accent)] text-white'
            : 'border-[var(--glass-border-strong)] text-transparent hover:border-[var(--accent)]'
        "
      >✓</span>
      <h3 class="flex-1 text-[15px] font-semibold text-[var(--text-1)] leading-snug truncate">
        {{ note.title || '无标题' }}
      </h3>
      <!-- 元数据状态点 -->
      <span
        v-if="note.meta_status === 'pending' || note.meta_status === 'processing'"
        class="pulse-dot mt-1.5 shrink-0"
        title="AI 正在生成元数据…"
      />
      <span
        v-else-if="note.meta_status === 'failed'"
        class="mt-1.5 w-2 h-2 rounded-full bg-[var(--danger)] shrink-0 cursor-pointer hover:scale-125 transition-transform"
        :title="'元数据生成失败：' + (note.meta_error || '未知错误') + '（点击重试）'"
        @click.stop="emit('retryMeta', note.id)"
      />
      <time class="text-[12px] text-[var(--text-3)] tabular-nums shrink-0 mt-0.5">
        {{ relTime(note.updated_at) }}
      </time>
    </div>

    <!-- 简介 -->
    <p
      v-if="note.summary"
      class="mt-1.5 text-[13px] text-[var(--text-2)] leading-relaxed line-clamp-2"
    >
      {{ note.summary }}
    </p>
    <p
      v-else-if="note.meta_status === 'pending' || note.meta_status === 'processing'"
      class="mt-1.5 space-y-1.5"
    >
      <span class="block h-3.5 rounded bg-[var(--glass-2)] animate-pulse w-full" />
      <span class="block h-3.5 rounded bg-[var(--glass-2)] animate-pulse w-2/3" />
    </p>

    <!-- 标签 + 实体 -->
    <div v-if="note.tags.length || note.entities.length" class="mt-2.5 flex flex-wrap gap-1.5">
      <span
        v-for="tag in note.tags"
        :key="'t' + tag"
        class="px-2 py-0.5 rounded-full text-[12px] text-[var(--accent)] border border-[color-mix(in_srgb,var(--accent)_40%,transparent)]"
      >#{{ tag }}</span>
      <button
        v-for="ent in note.entities"
        :key="'e' + ent.name"
        class="px-2 py-0.5 rounded-full text-[12px] transition-transform hover:scale-105"
        :style="{
          background: `color-mix(in srgb, ${entityColor(ent.type)} 12%, transparent)`,
          color: entityColor(ent.type),
        }"
        :title="`按实体「${ent.name}」过滤`"
        @click.stop="emit('filterEntity', ent.name)"
      >{{ ent.name }}</button>
    </div>
  </article>
</template>
