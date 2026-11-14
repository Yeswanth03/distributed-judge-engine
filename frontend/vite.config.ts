import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// Every backend call goes to the gateway, which checks the access token and routes to the owning service.
const GATEWAY = process.env.GATEWAY_URL ?? 'http://localhost:8000'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(import.meta.dirname, 'src') },
  },
  // Monaco is one ~2.7 MB chunk by nature; it loads only on pages with an editor.
  build: { chunkSizeWarningLimit: 3000 },
  server: {
    // 5500 is already in the User Service's OAUTH_ALLOWED_REDIRECTS, so Google/GitHub sign-in can return here.
    port: 5500,
    strictPort: true,
    proxy: {
      // The app calls /api/<gateway path>; the prefix keeps API paths apart from page URLs such as /problems/two-sum.
      '/api': {
        target: GATEWAY,
        changeOrigin: false,
        rewrite: (p) => p.replace(/^\/api/, ''),
        // The User Service scopes its refresh cookie to /auth; the browser requests it as /api/auth.
        cookiePathRewrite: { '/auth': '/api/auth' },
      },
    },
  },
})
