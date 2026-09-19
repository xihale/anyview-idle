import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

// build 产物直接落 web/dist, 由 Go 二进制 go:embed 托管 (单文件部署)。
// 开发: bun run dev 起 vite(:5173), /api 代理到本机 Go 服务(:8765)。
export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: "../web/dist",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8765",
    },
  },
});
