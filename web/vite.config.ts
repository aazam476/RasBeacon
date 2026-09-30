// Vite builds the web UI into dist/, which rbweb embeds.
// `npm run dev` starts a development server with instant reload. It
// forwards /api requests to an rbweb running on port 8080 of this computer
// (see CONTRIBUTING.md for how to start one).
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    // Keep dist/.gitkeep so the Go build works in a fresh checkout.
    emptyOutDir: false,
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8080",
    },
  },
});
