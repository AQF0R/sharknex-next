import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

// localBoundary 防止其他站点借助本地开发代理调用控制端。
function localBoundary(): Plugin {
  return {
    name: 'local-development-boundary',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const pathname = new URL(req.url || '/', 'http://127.0.0.1').pathname
        if (/(^|\/)\.env(?:[./]|$)/.test(pathname)) {
          res.statusCode = 403
          res.end('禁止访问环境配置')
          return
        }
        const host = req.headers.host
        const allowed = ['127.0.0.1:5174', 'localhost:5174']
        if (!host || !allowed.includes(host) || req.headers['sec-fetch-site'] === 'cross-site') {
          res.statusCode = 403
          res.end('仅允许本机同源访问')
          return
        }
        if (req.headers.origin && req.headers.origin !== 'http://' + host) {
          res.statusCode = 403
          res.end('拒绝跨站请求')
          return
        }
        next()
      })
    },
  }
}
export default defineConfig(({ mode }) => {
  // 不使用 VITE_ 前缀，令牌仅在 Node 代理进程内可见。
  const env = loadEnv(mode, fileURLToPath(new URL('..', import.meta.url)), '')
  const token = process.env.ENGINE_API_TOKEN || env.ENGINE_API_TOKEN
  return {
    plugins: [localBoundary(), vue()],
    resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
    server: {
      host: '127.0.0.1',
      port: 5174,
      strictPort: true,
      cors: false,
      proxy: {
        '/api': {
          target: 'http://127.0.0.1:8080',
          configure(proxy) {
            proxy.on('proxyReq', (request, source) => {
              if (!source.headers.authorization && token)
                request.setHeader('Authorization', 'Bearer ' + token)
            })
          },
        },
        '/healthz': { target: 'http://127.0.0.1:8080' },
        '/readyz': { target: 'http://127.0.0.1:8080' },
      },
    },
  }
})
