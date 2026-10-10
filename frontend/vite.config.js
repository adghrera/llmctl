import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
// The frontend builds into the Go module so the daemon can embed it:
//   frontend/  --(vite build)-->  internal/ui/dist/  --(go:embed)-->  binary
// `base: "./"` keeps asset URLs relative so the SPA works from any mount path.
export default defineConfig({
    plugins: [react()],
    base: "./",
    build: {
        outDir: "../internal/ui/dist",
        emptyOutDir: true,
    },
    server: {
        // Dev mode: proxy API + gateway to a running daemon (llmctl up).
        proxy: {
            "/api": "http://127.0.0.1:8080",
            "/v1": "http://127.0.0.1:8080",
            "/healthz": "http://127.0.0.1:8080",
        },
    },
});
