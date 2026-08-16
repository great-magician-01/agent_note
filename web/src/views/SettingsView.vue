<script setup lang="ts">
import { onMounted, ref } from 'vue'
import TopBar from '../components/TopBar.vue'
import { useAIConfigStore, type AIConfig, type AIConfigPayload } from '../stores/aiConfigs'

const store = useAIConfigStore()

// ---- 配置表单 ----
const showForm = ref(false)
const editingId = ref('')
const form = ref<AIConfigPayload>({ name: '', base_url: '', api_key: '', model: '' })
const formError = ref('')
const saving = ref(false)

function openCreate() {
  editingId.value = ''
  form.value = { name: '', base_url: '', api_key: '', model: '' }
  formError.value = ''
  showForm.value = true
}

function openEdit(cfg: AIConfig) {
  editingId.value = cfg.id
  // api_key 是脱敏值：编辑时留空表示不修改 → 简化处理：要求重新输入
  form.value = { name: cfg.name, base_url: cfg.base_url, api_key: '', model: cfg.model }
  formError.value = ''
  showForm.value = true
}

async function submit() {
  const f = form.value
  if (!f.name || !f.base_url || !f.model) {
    formError.value = '名称 / Base URL / Model 必填'
    return
  }
  if (!editingId.value && !f.api_key) {
    formError.value = 'API Key 必填'
    return
  }
  if (editingId.value && !f.api_key) {
    formError.value = '编辑时需重新输入 API Key（原密钥不回显）'
    return
  }
  saving.value = true
  formError.value = ''
  try {
    if (editingId.value) {
      await store.update({ ...f, id: editingId.value })
    } else {
      await store.create(f)
    }
    showForm.value = false
  } catch (e: any) {
    formError.value = e.response?.data?.error || '保存失败'
  } finally {
    saving.value = false
  }
}

async function remove(cfg: AIConfig) {
  if (!confirm(`删除配置「${cfg.name}」？`)) return
  await store.remove(cfg.id)
}

async function activate(cfg: AIConfig) {
  if (cfg.active === 1) return
  await store.activate(cfg.id)
}

// ---- 测试连接 ----
const testingId = ref('')
const testResult = ref<Record<string, { ok: boolean; error?: string }>>({})

async function test(cfg: AIConfig) {
  testingId.value = cfg.id
  try {
    const res = await store.test(cfg.id)
    testResult.value[cfg.id] = res
  } catch (e: any) {
    testResult.value[cfg.id] = { ok: false, error: e.message || '请求失败' }
  } finally {
    testingId.value = ''
  }
}

// ---- 主题 ----
const theme = ref(localStorage.getItem('theme') || 'dark')
function setTheme(t: string) {
  theme.value = t
  localStorage.setItem('theme', t)
  document.documentElement.dataset.theme = t
}

// ---- 楷体预览 ----
const serifPreview = ref(localStorage.getItem('serifPreview') !== 'off')
function setSerifPreview(on: boolean) {
  serifPreview.value = on
  if (on) {
    localStorage.removeItem('serifPreview')
    delete document.documentElement.dataset.serifPreview
  } else {
    localStorage.setItem('serifPreview', 'off')
    document.documentElement.dataset.serifPreview = 'off'
  }
}

onMounted(() => store.fetch())
</script>

<template>
  <div class="h-full flex flex-col items-center px-4">
    <div class="w-full max-w-[70%] flex flex-col h-full py-6">
      <TopBar :back="true">
        <template #title>
          <span class="text-[16px] font-semibold">设置</span>
        </template>
      </TopBar>

      <main class="glass reveal reveal-2 rounded-[20px] flex-1 mt-3 overflow-y-auto p-6 space-y-8">
        <!-- AI 配置 -->
        <section>
          <div class="flex items-center justify-between mb-4">
            <h2 class="text-[15px] font-semibold">AI 配置</h2>
            <button
              class="h-8 px-3 rounded-[8px] text-[12px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
              @click="openCreate"
            >＋ 新增配置</button>
          </div>
          <p class="text-[12px] text-[var(--text-3)] mb-4">OpenAI 兼容接口；可配置多个，同一时间仅一个生效。</p>

          <div class="space-y-3">
            <div
              v-for="cfg in store.list"
              :key="cfg.id"
              class="glass-2 rounded-[14px] p-4 transition-colors"
              :class="{ '!border-[var(--accent)]': cfg.active === 1 }"
            >
              <div class="flex items-center gap-3">
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2">
                    <span class="text-[14px] font-semibold truncate">{{ cfg.name }}</span>
                    <span
                      v-if="cfg.active === 1"
                      class="flex items-center gap-1.5 text-[11px] text-[var(--accent)]"
                    >
                      <span class="w-1.5 h-1.5 rounded-full bg-[var(--accent)]" />使用中
                    </span>
                  </div>
                  <p class="text-[12px] text-[var(--text-2)] truncate mt-1">
                    {{ cfg.model }} · {{ cfg.base_url }}
                  </p>
                  <!-- 测试结果 -->
                  <p
                    v-if="testResult[cfg.id]"
                    class="text-[12px] mt-1"
                    :class="testResult[cfg.id].ok ? 'text-[var(--success)]' : 'text-[var(--danger)]'"
                  >
                    {{ testResult[cfg.id].ok ? '✓ 连接成功' : '✕ ' + testResult[cfg.id].error }}
                  </p>
                </div>

                <button
                  class="h-8 px-3 rounded-[8px] text-[12px] text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors disabled:opacity-40"
                  :disabled="testingId === cfg.id"
                  @click="test(cfg)"
                >{{ testingId === cfg.id ? '测试中…' : '测试连接' }}</button>
                <button
                  v-if="cfg.active !== 1"
                  class="h-8 px-3 rounded-[8px] text-[12px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all"
                  @click="activate(cfg)"
                >启用</button>
                <button
                  class="h-8 px-3 rounded-[8px] text-[12px] text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)] hover:text-[var(--text-1)] transition-colors"
                  @click="openEdit(cfg)"
                >编辑</button>
                <button
                  class="h-8 px-3 rounded-[8px] text-[12px] text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)] hover:text-[var(--danger)] transition-colors"
                  @click="remove(cfg)"
                >删除</button>
              </div>
            </div>

            <p v-if="store.loaded && store.list.length === 0" class="text-center text-[13px] text-[var(--text-3)] py-8">
              还没有 AI 配置，点击右上角「新增配置」
            </p>
          </div>
        </section>

        <!-- 外观 -->
        <section>
          <h2 class="text-[15px] font-semibold mb-4">外观</h2>
          <div class="flex gap-2">
            <button
              v-for="t in [{ v: 'dark', label: '深色 · 夜墨' }, { v: 'light', label: '浅色 · 晨雾' }]"
              :key="t.v"
              class="h-9 px-4 rounded-[10px] text-[13px] transition-all"
              :class="theme === t.v
                ? 'text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] font-semibold'
                : 'text-[var(--text-2)] border border-[var(--glass-border)] hover:bg-[var(--glass-2)]'"
              @click="setTheme(t.v)"
            >{{ t.label }}</button>
          </div>
          <label class="mt-4 flex items-center gap-2.5 cursor-pointer select-none">
            <input
              type="checkbox"
              :checked="serifPreview"
              class="w-4 h-4 accent-[var(--accent)]"
              @change="setSerifPreview(($event.target as HTMLInputElement).checked)"
            />
            <span class="text-[13px] text-[var(--text-2)]">预览正文使用楷体（霞鹜文楷）</span>
          </label>
        </section>

        <!-- 关于 -->
        <section>
          <h2 class="text-[15px] font-semibold mb-2">关于</h2>
          <p class="text-[12px] text-[var(--text-3)]">AI 智能笔记 · v1.0</p>
        </section>
      </main>
    </div>

    <!-- 配置表单弹窗 -->
    <Teleport to="body">
      <div
        v-if="showForm"
        class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-[8px]"
        @click.self="showForm = false"
      >
        <div class="glass-2 rounded-[20px] w-[440px] p-6 reveal">
          <h3 class="text-[15px] font-semibold mb-5">{{ editingId ? '编辑配置' : '新增配置' }}</h3>

          <div class="space-y-3">
            <div>
              <label class="block text-[12px] text-[var(--text-2)] mb-1.5">名称</label>
              <input v-model="form.name" type="text" placeholder="如：DeepSeek / 公司网关"
                class="w-full h-10 px-3 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[13px] focus:border-[var(--glass-border-strong)] focus:outline-none" />
            </div>
            <div>
              <label class="block text-[12px] text-[var(--text-2)] mb-1.5">Base URL</label>
              <input v-model="form.base_url" type="text" placeholder="https://api.openai.com/v1"
                class="w-full h-10 px-3 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[13px] focus:border-[var(--glass-border-strong)] focus:outline-none" />
            </div>
            <div>
              <label class="block text-[12px] text-[var(--text-2)] mb-1.5">API Key</label>
              <input v-model="form.api_key" type="password" :placeholder="editingId ? '重新输入密钥' : 'sk-...'"
                class="w-full h-10 px-3 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[13px] focus:border-[var(--glass-border-strong)] focus:outline-none" />
            </div>
            <div>
              <label class="block text-[12px] text-[var(--text-2)] mb-1.5">Model</label>
              <input v-model="form.model" type="text" placeholder="如：gpt-4o-mini / deepseek-chat"
                class="w-full h-10 px-3 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[13px] focus:border-[var(--glass-border-strong)] focus:outline-none" />
            </div>
          </div>

          <p v-if="formError" class="mt-3 text-[12px] text-[var(--danger)]">{{ formError }}</p>

          <div class="mt-6 flex justify-end gap-2">
            <button
              class="h-9 px-4 rounded-[10px] text-[13px] text-[var(--text-2)] hover:bg-[var(--glass-2)] transition-colors"
              @click="showForm = false"
            >取消</button>
            <button
              class="h-9 px-4 rounded-[10px] text-[13px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 transition-all disabled:opacity-50"
              :disabled="saving"
              @click="submit"
            >{{ saving ? '保存中…' : '保存' }}</button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
