import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [vue(), tailwindcss()],
  test: {
    environment: 'happy-dom',
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:7562',
      '/uploads': 'http://localhost:7562',
    },
  },
})
