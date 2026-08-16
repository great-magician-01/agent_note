// 认证 store 单元测试：mock axios 实例，验证 state 与 localStorage 同步
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAuthStore } from './auth'
import api from '../api/client'

vi.mock('../api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

describe('auth store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
    vi.clearAllMocks()
  })

  it('login 成功写入 state 与 localStorage', async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { token: 'tok', username: 'u' } })
    const store = useAuthStore()
    expect(store.isLoggedIn).toBe(false)

    await store.login('u', 'p')

    expect(api.post).toHaveBeenCalledWith('/auth/login', { username: 'u', password: 'p' })
    expect(store.token).toBe('tok')
    expect(store.username).toBe('u')
    expect(localStorage.getItem('token')).toBe('tok')
    expect(localStorage.getItem('username')).toBe('u')
    expect(store.isLoggedIn).toBe(true)
  })

  it('logout 清空 state 与 localStorage', async () => {
    vi.mocked(api.post).mockResolvedValue({ data: { token: 'tok', username: 'u' } })
    const store = useAuthStore()
    await store.login('u', 'p')

    store.logout()

    expect(store.token).toBe('')
    expect(store.username).toBe('')
    expect(localStorage.getItem('token')).toBeNull()
    expect(localStorage.getItem('username')).toBeNull()
    expect(store.isLoggedIn).toBe(false)
  })

  it('isLoggedIn 随 token 变化', () => {
    const store = useAuthStore()
    expect(store.isLoggedIn).toBe(false)
    store.token = 'abc'
    expect(store.isLoggedIn).toBe(true)
    store.token = ''
    expect(store.isLoggedIn).toBe(false)
  })
})
