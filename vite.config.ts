import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')

  return {
    root: 'web',
    publicDir: 'public',
    plugins: [vue()],
    base: env.VITE_BASE_PATH || '/',
    build: {
      outDir: '../public',
      emptyOutDir: false,
      minify: false,
    },
    server: {
      proxy: {
        '/api': {
          target: env.VITE_API_TARGET || 'http://127.0.0.1:8080',
          changeOrigin: true,
        },
      },
    },
  }
})
