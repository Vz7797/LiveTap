import path from "node:path";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: { host: "127.0.0.1", proxy: { "/admin/api": { target: "http://127.0.0.1:8090", ws: true } } },
  preview: { proxy: { "/admin/api": { target: "http://127.0.0.1:8090", ws: true } } },
});
