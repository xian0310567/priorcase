import { defineConfig } from "vitest/config";

// jsdom 을 쓰는 이유는 Task 5 부터다 — 화면 함수가 document 를 만진다.
// 지금(포맷 함수)은 필요 없지만, 뒤에서 환경을 바꾸면 그 커밋에서 무엇이
// 깨졌는지 섞여 보인다. 처음부터 최종 환경으로 둔다.
export default defineConfig({
  test: { environment: "jsdom", include: ["tests/**/*.test.ts"] },
});
