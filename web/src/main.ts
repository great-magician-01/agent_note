import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
// 本地字体（不依赖外网）
import '@fontsource/inter/400.css'
import '@fontsource/inter/600.css'
import '@fontsource/inter/700.css'
import '@fontsource/jetbrains-mono/400.css'
import 'lxgw-wenkai-screen-webfont/lxgwwenkaiscreen.css'
import App from './App.vue'
import router from './router'

// 主题初始化（默认深色）
const theme = localStorage.getItem('theme') || 'dark'
document.documentElement.dataset.theme = theme

// 楷体预览开关
if (localStorage.getItem('serifPreview') === 'off') {
  document.documentElement.dataset.serifPreview = 'off'
}

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')
