import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// jsdom 환경에서는 import.meta.url 이 file: 스킴이 아니다 — vitest 는 app/ 에서
// 도므로 cwd 기준으로 읽는다.
const css = readFileSync(resolve(process.cwd(), "src/style.css"), "utf8");

/** 토큰 정의부(:root 블록)와 주석을 뺀 나머지 — 실제로 색을 쓰는 자리다.
 *
 * **주석은 면제한다.** 이 저장소는 주석을 과거를 적는 자리로 쓰고(arch 의
 * 옛이름 검사가 같은 면제를 둔다), 이 파일의 주석에는 무엇이 왜 틀렸는지
 * 보이려고 옛 색값이 그대로 적혀 있다. */
const usage = css
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/@media[^{]*\{\s*:root\s*\{[^}]*\}\s*\}/g, "")
  .replace(/:root\s*\{[^}]*\}/g, "");

describe("색 토큰", () => {
  // ★ 2026-09-01: 색이 전부 밝은 배경 기준으로 하드코딩돼 있었다. OS 가 다크
  // 배경을 칠하는데 #666 본문(대비 2.90)·#0001 구분선(검정 6%, 안 보임)이라
  // 화면이 글자 벽이 됐다. **취향이 아니라 고장이었다.**
  it("쓰는 자리에는 하드코딩 색이 없다", () => {
    const hex = usage.match(/#[0-9a-fA-F]{3,8}\b/g) ?? [];
    expect(hex, `하드코딩된 색: ${hex.join(", ")}`).toEqual([]);
  });

  it("라이트가 정의한 **색** 토큰을 다크가 전부 덮는다", () => {
    // **이름이 아니라 값으로 가른다.** `--radius`·`--gap` 처럼 색이 아닌 토큰은
    // 다크에서 덮을 이유가 없다. 이름 규칙으로 면제하면 그 규칙을 어긴 새 색이
    // 조용히 빠져나간다 — 이 검사가 막으려는 것이 정확히 그것이다.
    const decls = (block: string) =>
      new Map([...block.matchAll(/(--[a-z-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
    const light = css.match(/:root\s*\{([^}]*)\}/)![1];
    const dark = css.match(/@media \(prefers-color-scheme: dark\)\s*\{\s*:root\s*\{([^}]*)\}/)![1];
    const isColor = (v: string) => /^(#[0-9a-fA-F]{3,8}|rgb|hsl|color-mix)/.test(v);
    const d = decls(dark);
    const missing = [...decls(light)]
      .filter(([, v]) => isColor(v))
      .map(([n]) => n)
      .filter((n) => !d.has(n));
    expect(missing, `다크에서 안 덮인 색: ${missing.join(", ")}`).toEqual([]);
  });

  it("배경과 글자색을 body 에 명시한다", () => {
    // color-scheme 만 선언하면 배경은 OS 가 칠하고 글자색은 우리가 정한 채로
    // 남는다 — 그 어긋남이 이 사고의 뿌리다.
    expect(css).toMatch(/body\s*\{[^}]*background:\s*var\(--bg\)/);
    expect(css).toMatch(/body\s*\{[^}]*color:\s*var\(--fg\)/);
  });
});
