import { defineStore } from 'pinia'
import api from '../api/client'

export interface AIConfig {
  id: string
  name: string
  base_url: string
  api_key: string // 脱敏后
  model: string
  active: number
}

export interface AIConfigPayload {
  id?: string
  name: string
  base_url: string
  api_key: string
  model: string
}

export const useAIConfigStore = defineStore('aiConfigs', {
  state: () => ({
    list: [] as AIConfig[],
    loaded: false,
  }),
  getters: {
    activeConfig: (s) => s.list.find((c) => c.active === 1),
  },
  actions: {
    async fetch() {
      const { data } = await api.get('/ai-configs')
      this.list = data || []
      this.loaded = true
    },
    async create(payload: AIConfigPayload) {
      await api.post('/ai-configs/create', payload)
      await this.fetch()
    },
    async update(payload: AIConfigPayload) {
      await api.post('/ai-configs/update', payload)
      await this.fetch()
    },
    async remove(id: string) {
      await api.post('/ai-configs/delete', { id })
      await this.fetch()
    },
    async activate(id: string) {
      await api.post('/ai-configs/activate', { id })
      await this.fetch()
    },
    async test(id: string): Promise<{ ok: boolean; error?: string }> {
      const { data } = await api.post('/ai-configs/test', { id })
      return data
    },
  },
})
