import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    // Emitted into web/dist, which the Go binary embeds.
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    // `npm run dev` proxies the API to a locally running argus binary, so
    // the frontend can hot-reload without rebuilding Go.
    proxy: {
      "/api": { target: "http://localhost:18474", changeOrigin: true },
    },
  },
});
