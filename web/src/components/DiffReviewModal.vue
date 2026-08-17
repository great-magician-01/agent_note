<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import * as Diff from 'diff'
import { toolLabel } from '../stores/chat'

// AI 正文修改提案的 diff 审核弹层：
// 行级 diff 逐行渲染（红删绿增），接受 → 由父组件落库；拒绝/Esc/点遮罩 → 丢弃
const props = defineProps<{
  oldContent: string
  newContent: string
  tool: string
}>()

const emit = defineEmits<{
  accept: []
  reject: []
  close: []
}>()

interface DiffLine {
  type: 'add' | 'del' | 'ctx'
  text: string
}

const lines = computed<DiffLine[]>(() => {
  const parts = Diff.diffLines(props.oldContent, props.newContent)
  const out: DiffLine[] = []
  for (const part of parts) {
    const type: DiffLine['type'] = part.added ? 'add' : part.removed ? 'del' : 'ctx'
    // diffLines 的 value 以 \n 结尾（最后一段除外），切分时丢掉末尾空串
    const segs = part.value.split('\n')
    if (segs[segs.length - 1] === '') segs.pop()
    for (const text of segs) out.push({ type, text })
  }
  return out
})

const addCount = computed(() => lines.value.filter((l) => l.type === 'add').length)
const delCount = computed(() => lines.value.filter((l) => l.type === 'del').length)

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-[8px] p-4"
      @click.self="emit('close')"
    >
      <div class="glass-2 rounded-[20px] w-[90vw] max-w-4xl h-[80vh] flex flex-col reveal overflow-hidden">
        <!-- 头部：工具名 + 增删统计 -->
        <div class="px-6 pt-5 pb-3 flex items-center gap-3 shrink-0 border-b border-[var(--glass-border)]">
          <h3 class="text-[15px] font-semibold">AI 修改提案 · {{ toolLabel(tool) }}</h3>
          <span class="text-[12px] tabular-nums">
            <span class="text-[var(--success)]">+{{ addCount }}</span>
            <span class="text-[var(--text-3)] mx-1">/</span>
            <span class="text-[var(--danger)]">-{{ delCount }}</span>
          </span>
          <span class="flex-1" />
          <span class="text-[12px] text-[var(--text-3)]">接受后自动保存</span>
        </div>

        <!-- diff 正文 -->
        <div class="flex-1 overflow-y-auto px-6 py-4 min-h-0 text-[12px] leading-[1.7]" style="font-family: var(--font-mono)">
          <div
            v-for="(line, i) in lines"
            :key="i"
            class="flex whitespace-pre-wrap break-all rounded-[3px] px-2 -mx-2"
            :class="
              line.type === 'add'
                ? 'bg-[color-mix(in_srgb,var(--success)_14%,transparent)] text-[var(--text-1)]'
                : line.type === 'del'
                  ? 'bg-[color-mix(in_srgb,var(--danger)_14%,transparent)] text-[var(--text-2)]'
                  : 'text-[var(--text-2)]'
            "
          >
            <span
              class="w-4 shrink-0 select-none"
              :class="
                line.type === 'add'
                  ? 'text-[var(--success)]'
                  : line.type === 'del'
                    ? 'text-[var(--danger)]'
                    : 'text-[var(--text-3)]'
              "
            >{{ line.type === 'add' ? '+' : line.type === 'del' ? '-' : ' ' }}</span>
            <span class="flex-1 min-w-0">{{ line.text || ' ' }}</span>
          </div>
        </div>

        <!-- 底部操作 -->
        <div class="px-6 py-4 flex justify-end gap-2 shrink-0 border-t border-[var(--glass-border)]">
          <button
            class="h-9 px-4 rounded-[10px] text-[13px] text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
            @click="emit('reject')"
          >拒绝</button>
          <button
            class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
            @click="emit('accept')"
          >接受修改</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>
