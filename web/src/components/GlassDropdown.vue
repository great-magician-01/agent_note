<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'

export interface GlassMenuItem {
  key: string
  label: string
  danger?: boolean
  /** 在该项上方渲染一条分隔线 */
  divider?: boolean
}

const props = withDefaults(
  defineProps<{
    items: GlassMenuItem[]
    /** 当前选中项的 key（选择器场景显示 ✓） */
    current?: string
    align?: 'left' | 'right'
    /** 面板弹出方向：bottom = 向下（默认），top = 向上 */
    placement?: 'bottom' | 'top'
    /** 面板最小宽度 */
    panelClass?: string
  }>(),
  { current: undefined, align: 'right', placement: 'bottom', panelClass: '' },
)

const emit = defineEmits<{
  select: [key: string]
}>()

const open = ref(false)
const root = ref<HTMLElement | null>(null)

function toggle() {
  open.value = !open.value
}

function pick(item: GlassMenuItem) {
  open.value = false
  emit('select', item.key)
}

function onDocDown(e: MouseEvent) {
  if (open.value && root.value && !root.value.contains(e.target as Node)) {
    open.value = false
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') open.value = false
}

onMounted(() => {
  document.addEventListener('mousedown', onDocDown)
  document.addEventListener('keydown', onKeydown)
})
onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onDocDown)
  document.removeEventListener('keydown', onKeydown)
})

defineExpose({ close: () => (open.value = false) })
void props
</script>

<template>
  <div ref="root" class="relative shrink-0">
    <span @click="toggle">
      <slot name="trigger" :open="open">
        <button
          class="w-9 h-9 rounded-full flex items-center justify-center text-[var(--text-2)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
          :class="open ? 'bg-[var(--glass-2)] text-[var(--text-1)]' : ''"
          title="更多"
          type="button"
        >⋯</button>
      </slot>
    </span>

    <Transition name="menu">
      <div
        v-if="open"
        class="absolute z-50 glass-3 rounded-[12px] py-1.5 min-w-[140px] shadow-[0_12px_40px_rgba(0,0,0,.35)]"
        :class="[
          align === 'right' ? 'right-0' : 'left-0',
          placement === 'top' ? 'bottom-full mb-2' : 'top-full mt-2',
          panelClass,
        ]"
      >
        <template v-for="item in items" :key="item.key">
          <div v-if="item.divider" class="my-1 h-px bg-[var(--glass-border)]" />
          <button
            type="button"
            class="w-full flex items-center gap-2 px-3.5 py-2 text-[13px] transition-colors"
            :class="
              item.danger
                ? 'text-[var(--danger)] hover:bg-[color-mix(in_srgb,var(--danger)_12%,transparent)]'
                : 'text-[var(--text-1)] hover:bg-[var(--glass-2)]'
            "
            @click="pick(item)"
          >
            <span class="flex-1 truncate text-center">{{ item.label }}</span>
            <span
              v-if="current === item.key"
              class="text-[var(--accent)] text-[12px] shrink-0"
            >✓</span>
          </button>
        </template>
      </div>
    </Transition>
  </div>
</template>

<style scoped>
.menu-enter-active,
.menu-leave-active {
  transition:
    opacity 0.15s ease,
    transform 0.15s ease;
}
.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px) scale(0.98);
}
</style>
