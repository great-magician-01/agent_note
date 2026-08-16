const BASE = 'AI 智能笔记'

// setTitle 设置浏览器标题：「子标题 · AI 智能笔记」
export function setTitle(sub?: string) {
  document.title = sub ? `${sub} · ${BASE}` : BASE
}
