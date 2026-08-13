import { defineConfig } from "vite";

export default defineConfig({
  // Tauri 가 이 포트를 고정으로 기대한다. 바꾸면 tauri.conf.json 도 같이 바꿔야 한다.
  server: { port: 5173, strictPort: true },
  build: { outDir: "dist", emptyOutDir: true },
});
