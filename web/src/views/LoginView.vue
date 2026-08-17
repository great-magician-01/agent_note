<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()

const username = ref('')
const password = ref('')
const error = ref('')
const loading = ref(false)

async function submit() {
  if (!username.value || !password.value) return
  loading.value = true
  error.value = ''
  try {
    await auth.login(username.value, password.value)
    router.push('/')
  } catch (e: any) {
    error.value = e.response?.data?.error || '登录失败，请检查网络'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="h-full flex items-center justify-center px-4">
    <div class="glass-2 reveal rounded-[20px] w-full max-w-[420px] px-6 sm:px-10 py-12">
      <!-- Logo -->
      <div class="text-center mb-8">
        <div
          class="text-[32px] font-bold bg-gradient-to-r from-[var(--aurora-teal)] to-[var(--aurora-violet)] bg-clip-text text-transparent"
        >
          ◈ Note
        </div>
        <p class="mt-2 text-[15px] text-[var(--text-2)]">回到你的知识库</p>
      </div>

      <form class="space-y-4" @submit.prevent="submit">
        <input
          v-model="username"
          type="text"
          placeholder="用户名"
          autocomplete="username"
          class="w-full h-11 px-4 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[var(--text-1)] placeholder:text-[var(--text-3)] focus:border-[var(--glass-border-strong)] focus:outline-none transition-colors"
        />
        <input
          v-model="password"
          type="password"
          placeholder="密码"
          autocomplete="current-password"
          class="w-full h-11 px-4 rounded-[10px] bg-[var(--glass-2)] border border-[var(--glass-border)] text-[var(--text-1)] placeholder:text-[var(--text-3)] focus:border-[var(--glass-border-strong)] focus:outline-none transition-colors"
          :class="{ '!border-[var(--danger)]': error }"
        />

        <p v-if="error" class="text-[13px] text-[var(--danger)]">{{ error }}</p>

        <button
          type="submit"
          :disabled="loading || !username || !password"
          class="w-full h-11 rounded-[10px] font-semibold text-white bg-gradient-to-br from-[var(--accent)] to-[var(--accent-deep)] hover:brightness-110 active:brightness-95 disabled:opacity-50 disabled:cursor-not-allowed transition-all"
        >
          {{ loading ? '登录中…' : '登录' }}
        </button>
      </form>
    </div>
  </div>
</template>
