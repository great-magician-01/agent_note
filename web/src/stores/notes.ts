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

// fetch 请求序号：只接受最后一次请求的响应落地（防竞态）
let fetchSeq = 0

export const useNoteStore = defineStore('notes', {
  state: () => ({
    items: [] as NoteItem[],
    total: 0,
    loading: false,
    error: null as string | null,
    keyword: '',
    entityFilter: '',
    page: 1,
    pageSize: 30,
  }),
  getters: {
    hasMore: (s) => s.items.length < s.total,
  },
  actions: {
    async fetch(categoryId?: string, append = false) {
      const seq = ++fetchSeq
      if (!append) this.page = 1
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
        if (seq !== fetchSeq) return // 已有更新的请求，丢弃本次响应
        this.items = append ? [...this.items, ...(data.items || [])] : data.items || []
        this.total = data.total || 0
        this.error = null
      } catch (e: any) {
        if (seq !== fetchSeq) return
        if (append) this.page-- // 追加失败回滚页码，便于重试
        this.error = e.response?.data?.error || '加载失败，请检查网络后重试'
      } finally {
        if (seq === fetchSeq) this.loading = false
      }
    },
    // 无限滚动：下一页追加到 items
    async loadMore(categoryId?: string) {
      if (this.loading || !this.hasMore) return
      this.page++
      await this.fetch(categoryId, true)
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
