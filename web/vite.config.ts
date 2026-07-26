import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: { "/api": "http://localhost:4530", "/rest": "http://localhost:4530", "/ws": "ws://localhost:4530" },
    host: "0.0.0.0",
    port: 5173,
  },
})
