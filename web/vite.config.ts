import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { fileURLToPath, URL } from "node:url";

const apiTarget = process.env.DAUTH_API_TARGET || "http://127.0.0.1:8681";
const webPort = Number(process.env.WEB_PORT || 8682);
const usePolling = process.env.DAUTH_VITE_POLL === "1";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@donktech/d-auth-sdk": fileURLToPath(new URL("../packages/sdk/src/index.ts", import.meta.url)),
      "@donktech/d-auth-admin": fileURLToPath(new URL("../packages/admin/src/index.ts", import.meta.url)),
    },
  },
  optimizeDeps: {
    exclude: ["@donktech/d-auth-admin", "@donktech/d-auth-sdk"],
  },
  server: {
    host: "0.0.0.0",
    port: webPort,
    strictPort: true,
    fs: { allow: [".."] },
    watch: { usePolling },
    proxy: {
      "/v1": { target: apiTarget, changeOrigin: true },
      "/health": { target: apiTarget, changeOrigin: true },
      "/static": { target: apiTarget, changeOrigin: true },
    },
  },
});
