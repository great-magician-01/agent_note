import { createRouter, createWebHistory } from 'vue-router'
import { setTitle } from '../composables/useTitle'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('../views/LoginView.vue'),
      meta: { title: '登录' },
    },
    {
      path: '/',
      name: 'home',
      component: () => import('../views/HomeView.vue'),
      // 首页标题由 HomeView 按当前分类动态设置
    },
    {
      path: '/note/new',
      name: 'note-new',
      component: () => import('../views/EditorView.vue'),
      meta: { title: '新建笔记' },
    },
    {
      path: '/note/:id',
      name: 'note-edit',
      component: () => import('../views/EditorView.vue'),
      // 标题由 EditorView 按笔记标题/模式动态设置
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('../views/SettingsView.vue'),
      meta: { title: '设置' },
    },
  ],
})

router.beforeEach((to) => {
  const token = localStorage.getItem('token')
  if (!token && to.name !== 'login') return { name: 'login' }
  if (token && to.name === 'login') return { name: 'home' }
})

// 静态页面标题（动态页面的视图内 watch 会覆盖）
router.afterEach((to) => {
  setTitle(to.meta.title as string | undefined)
})

export default router
