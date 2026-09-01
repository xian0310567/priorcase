import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const css = readFileSync(resolve(process.cwd(), "src/style.css"), "utf8");

/** ★★★ **두 칸 화면은 스크롤이 하나여야 한다.**
 *
 * 2026-09-01 사용자 지적: 설정 화면에 스크롤 막대가 **두 개** 보였다.
 *
 * 원인은 선택자였다. `#app` 의 여백을 걷어내고 높이를 화면에 맞추는 규칙이
 * `body:has(.browse)` 로 걸려 있었는데, 설정 화면의 껍데기는 `.browse` 가 아니라
 * `.settings-shell` 이다. 그래서 설정에서는 `#app` 이 여백을 그대로 쥔 채 100vh
 * 짜리 자식을 담았고, 그 초과분이 **바깥 스크롤**을 하나 더 만들었다. 안쪽은
 * 본문 칸(`.pane2 > *`)의 스크롤이다.
 *
 * 골격을 `.pane2` 로 공유했으므로 이 규칙도 그것을 봐야 한다 — **화면이 늘 때마다
 * 이 선택자를 고쳐야 하는 구조 자체가 고장이다.** */
describe("두 칸 화면의 스크롤", () => {
  it("전체를 채우는 규칙이 공유 골격(.pane2)에 걸린다", () => {
    // 주석은 과거를 적는 자리라 면제한다 (palette.test.ts 와 같은 규칙).
    const code = css.replace(/\/\*[\s\S]*?\*\//g, "");
    const rule = code.match(/body:has\(([^)]+)\)\s*#app/);
    expect(rule, "#app 을 화면에 맞추는 규칙이 없다").toBeTruthy();
    expect(rule![1], "특정 화면 이름에 걸려 있다 — 다른 두 칸 화면에서 스크롤이 둘이 된다")
      .toContain(".pane2");
  });

  it("두 칸 골격이 화면 높이를 쓴다", () => {
    expect(css).toMatch(/\.pane2\s*\{[^}]*height:\s*100vh/);
  });
});
