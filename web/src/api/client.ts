import axios from 'axios'
import router from '../router'

const api = axios.create({ baseURL: '/api', timeout: 30_000 })

// 请求拦截：自动带 token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token')
  if (token) config.headers.Authorization = `Bearer ${token}`
  return config
})

// 响应拦截：401 清 token 跳登录（router.push，不整页刷新，避免丢失未保存内容）
api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token')
      if (router.currentRoute.value.name !== 'login') router.push('/login').catch(() => {})
    }
    return Promise.reject(err)
  },
)

export default api
