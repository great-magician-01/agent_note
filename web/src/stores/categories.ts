import { defineStore } from 'pinia'
import api from '../api/client'

export interface Category {
  id: string
  name: string
  sort: number
  note_count: number
}

export const useCategoryStore = defineStore('categories', {
  state: () => ({
    list: [] as Category[],
    activeId: '' as string, // '' = 全部；'0' = 未分类
    loaded: false,
  }),
  actions: {
    async fetch() {
      const { data } = await api.get('/categories')
      this.list = data || []
      this.loaded = true
    },
    async create(name: string) {
      await api.post('/categories/create', { name })
      await this.fetch()
    },
    async rename(id: string, name: string) {
      await api.post('/categories/update', { id, name })
      await this.fetch()
    },
    async remove(id: string) {
      await api.post('/categories/delete', { id })
      if (this.activeId === id) this.activeId = ''
      await this.fetch()
    },
    select(id: string) {
      this.activeId = id
    },
  },
})
