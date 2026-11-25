import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";
export default defineConfig({
  plugins: [react(), tailwind()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: { geography: ["leaflet", "leaflet.markercluster"] },
      },
    },
  },
  server: {
    proxy: {
      "/hybrid-doctor": "http://127.0.0.1:8000",
      "/api": "http://127.0.0.1:8000",
      "/ws": { target: "ws://127.0.0.1:8000", ws: true },
    },
  },
});
