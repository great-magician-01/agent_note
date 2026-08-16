import { defineStore } from 'pinia'
import api from '../api/client'

export interface EntityVO {
  name: string
  type: string
}

export interface NoteItem {
  id: string
  title: string
  summary: string
  meta_status: 'none' | 'pending' | 'processing' | 'done' | 'failed'
  meta_error?: string
  tags: string[]
  entities: EntityVO[]
  category_id: string | null
  content_md?: string
  created_at: string
  updated_at: string
}

export interface NoteListParams {
  category_id?: string
  keyword?: string
  tag?: string
  entity?: string
  page?: number
  page_size?: number
}

export const useNoteStore = defineStore('notes', {
  state: () => ({
    items: [] as NoteItem[],
    total: 0,
    loading: false,
    keyword: '',
    entityFilter: '',
    page: 1,
    pageSize: 30,
  }),
  actions: {
    async fetch(categoryId?: string) {
      this.loading = true
      try {
        const params: NoteListParams = {
          page: this.page,
          page_size: this.pageSize,
        }
        if (categoryId) params.category_id = categoryId
        if (this.keyword) params.keyword = this.keyword
        if (this.entityFilter) params.entity = this.entityFilter
        const { data } = await api.get('/notes', { params })
        this.items = data.items || []
        this.total = data.total || 0
      } finally {
        this.loading = false
      }
    },
    setKeyword(kw: string) {
      this.keyword = kw
      this.page = 1
    },
    setEntityFilter(name: string) {
      this.entityFilter = name
      this.page = 1
    },
    async create(content: string, title = '', categoryId?: string | null) {
      const { data } = await api.post('/notes/create', {
        title,
        content,
        category_id: categoryId ?? null,
      })
      return data as NoteItem
    },
    async update(id: string, title: string, content: string, categoryId: string | null) {
      const { data } = await api.post('/notes/update', {
        id,
        title,
        content,
        category_id: categoryId,
      })
      return data as NoteItem
    },
    async remove(id: string) {
      await api.post('/notes/delete', { id })
      this.items = this.items.filter((n) => n.id !== id)
      this.total = Math.max(0, this.total - 1)
    },
    async batchRemove(ids: string[]) {
      await api.post('/notes/batch/delete', { ids })
      this.items = this.items.filter((n) => !ids.includes(n.id))
      this.total = Math.max(0, this.total - ids.length)
    },
    async batchMove(ids: string[], categoryId: string | null) {
      await api.post('/notes/batch/move', { ids, category_id: categoryId })
    },
    async getOne(id: string) {
      const { data } = await api.get(`/notes/${id}`)
      return data as NoteItem
    },
    async regenerateMeta(id: string) {
      await api.post('/notes/meta/regenerate', { id })
    },
  },
})
