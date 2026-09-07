import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  base: "/lighthouse/",
  resolve: {
    alias: {
      "@": resolve(import.meta.dirname, "src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/lighthouse/api": {
        target: "http://localhost:3000",
        ws: true,
      },
      "/lighthouse/ws": {
        target: "http://localhost:3000",
        ws: true,
      },
      "/lighthouse/auth": {
        target: "http://localhost:3000",
        ws: false,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
