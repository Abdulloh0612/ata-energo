import { defineConfig } from 'vite';

export default defineConfig({
  publicDir: false,
  build: {
    outDir: 'static/build',
    manifest: true,
    rollupOptions: {
      input: 'frontend/main.ts',
    },
  },
});
