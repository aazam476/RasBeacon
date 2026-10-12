// Vite builds the web UI into dist/, which rbweb embeds (see embed.go).
// `npm run dev` starts a development server with instant reload. It
// forwards /api requests to an rbweb running on port 8080 of this computer
// (see CONTRIBUTING.md for how to start one).
import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    // Keep dist/.gitkeep so the Go build works in a fresh checkout (`make ui`
    // clears the old files first).
    emptyOutDir: false,
    rolldownOptions: {
      output: {
        // React and the router in a file of their own: it stays the same from one RasBeacon
        // release to the next, so browsers keep it cached when only the app changes.
        codeSplitting: {
          groups: [{ name: 'vendor', test: /node_modules/ }],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
});
