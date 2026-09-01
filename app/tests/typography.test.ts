import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const css = readFileSync(resolve(process.cwd(), "src/style.css"), "utf8");
const usage = css
  .replace(/\/\*[\s\S]*?\*\//g, "")
  .replace(/@media[^{]*\{\s*:root\s*\{[^}]*\}\s*\}/g, "")
  .replace(/:root\s*\{[^}]*\}/g, "");

/** ★ **글자 크기는 척도에서만 나온다.**
 *
 * 옵시디언은 UI(13px)와 본문(16px)의 크기를 따로 둔다. 이 앱은 그 구별이 없어
 * 값이 열두 종(11·12·12.5·13·13.5·14·15·16·19·24·32·34)으로 흩어져 있었고,
 * 손으로 고른 큰 값들이 화면을 무겁게 만들었다 — 사업주가 "전체적으로 폰트가
 * 너무 크다" 고 한 화면이 그것이다(2026-09-01).
 *
 * 척도가 없으면 고칠 때마다 새 값이 하나씩 는다. 여기서 막는다. */
describe("글자 척도", () => {
  it("쓰는 자리에는 px 로 박은 글자 크기가 없다", () => {
    const hard = usage.match(/font-size:\s*[0-9.]+px/g) ?? [];
    expect(hard, `척도 밖의 크기: ${hard.join(", ")}`).toEqual([]);
  });

  it("척도 여섯 급이 다 있다", () => {
    for (const t of ["--fs-xs", "--fs-sm", "--fs-ui", "--fs-md", "--fs-lg", "--fs-xl"]) {
      expect(css, `${t} 가 없다`).toContain(`${t}:`);
    }
  });

  it("UI 는 옵시디언의 13px, 본문은 그보다 크다", () => {
    const px = (name: string): number =>
      Number(/([0-9.]+)px/.exec(new RegExp(`${name}:\\s*([^;]+);`).exec(css)![1])![1]);
    expect(px("--fs-ui"), "UI 기본이 옵시디언(--font-ui-small)과 다르다").toBe(13);
    // 읽는 자리는 UI 보다 커야 한다 — 같아지면 글과 껍데기가 구별되지 않는다.
    expect(px("--fs-md")).toBeGreaterThan(px("--fs-ui"));
    expect(px("--fs-lg")).toBeGreaterThan(px("--fs-md"));
    expect(px("--fs-xl")).toBeGreaterThan(px("--fs-lg"));
  });

  it("급이 단조 증가한다", () => {
    const order = ["--fs-xs", "--fs-sm", "--fs-ui", "--fs-md", "--fs-lg", "--fs-xl"];
    const vals = order.map((n) =>
      Number(/([0-9.]+)px/.exec(new RegExp(`${n}:\\s*([^;]+);`).exec(css)![1])![1]),
    );
    for (let i = 1; i < vals.length; i++) {
      expect(vals[i], `${order[i]} 가 ${order[i - 1]} 보다 작다`).toBeGreaterThan(vals[i - 1]);
    }
  });
});
