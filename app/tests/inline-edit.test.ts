import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { markdown } from "../src/render/markdown";

/** ★★★ **블록마다 원문 줄 범위를 안다.**
 *
 * 옵시디언·노션은 "보기 모드/편집 모드" 가 없다 — 누른 자리가 그 자리에서 원문이
 * 되고 나머지는 렌더된 채로 남는다. 그러려면 렌더러가 **각 블록이 원문의 몇 번째
 * 줄에서 왔는지**를 들고 있어야 한다. 그 범위가 틀리면 고친 글이 엉뚱한 줄을
 * 덮어써서 **결정문이 조용히 망가진다** — 되돌릴 수 없는 종류다. */
describe("블록 줄 범위", () => {
  const range = (el: Element) => [Number((el as HTMLElement).dataset.from), Number((el as HTMLElement).dataset.to)];

  it("문단·제목이 자기 줄을 가리킨다", () => {
    const src = "## 제목\n\n첫 문단\n이어지는 줄\n\n둘째 문단\n";
    const { root } = markdown(src);
    const bs = [...root.children];
    expect(range(bs[0])).toEqual([0, 1]);   // ## 제목
    expect(range(bs[1])).toEqual([2, 4]);   // 첫 문단 (두 줄)
    expect(range(bs[2])).toEqual([5, 6]);   // 둘째 문단
  });

  it("코드 블록은 울타리까지 자기 것이다", () => {
    const src = "앞\n\n```bash\norca open\n```\n\n뒤\n";
    const { root } = markdown(src);
    const code = root.querySelector("pre.md-code")!;
    expect(range(code)).toEqual([2, 5]);
  });

  it("목록 전체가 한 블록이다 — 중첩까지", () => {
    const src = "- 하나\n  - 안쪽\n- 둘\n\n뒤 문단\n";
    const { root } = markdown(src);
    expect(range(root.querySelector("ul")!)).toEqual([0, 3]);
  });

  it("표 전체가 한 블록이다", () => {
    const src = "| a | b |\n| --- | --- |\n| 1 | 2 |\n\n뒤\n";
    const { root } = markdown(src);
    expect(range(root.querySelector(".md-tablewrap")!)).toEqual([0, 3]);
  });

  // ★★ **범위로 원문을 잘라내면 그 블록의 원문이 그대로 나와야 한다.**
  // 이것이 성립해야 "고친 줄만 갈아 끼우기" 가 안전하다.
  it("모든 블록의 범위가 원문을 빠짐없이·겹치지 않게 덮는다", () => {
    const src = [
      "## 제목", "", "문단 하나", "이어짐", "", "- 목록", "  - 안쪽", "",
      "| a | b |", "| --- | --- |", "| 1 | 2 |", "", "> 인용", "",
      "```go", "x := 1", "```", "", "\t들여쓴 표", "", "끝 문단",
    ].join("\n");
    const { root, blocks } = markdown(src);
    const lines = src.split("\n");
    expect(blocks.length).toBe(root.children.length);
    let prevTo = 0;
    for (const b of blocks) {
      expect(b.from, `블록이 겹치거나 뒤로 갔다 (from=${b.from}, 앞 to=${prevTo})`).toBeGreaterThanOrEqual(prevTo);
      expect(b.to).toBeGreaterThan(b.from);
      // 그 범위를 잘라 다시 그리면 같은 종류의 요소 하나가 나와야 한다.
      const slice = lines.slice(b.from, b.to).join("\n");
      const again = markdown(slice).root;
      expect(again.children.length, `잘라낸 원문이 한 블록이 아니다: ${JSON.stringify(slice)}`).toBe(1);
      expect(again.children[0].tagName).toBe(b.el.tagName);
      prevTo = b.to;
    }
    expect(prevTo).toBeLessThanOrEqual(lines.length);
  });

  it("블록 요소에 data-from·data-to 가 실제로 붙는다", () => {
    const { root } = markdown("문단\n");
    const el = root.children[0] as HTMLElement;
    expect(el.dataset.from).toBe("0");
    expect(el.dataset.to).toBe("1");
  });
});

/** ★ 줄 갈아 끼우기 — 고친 블록만 원문에서 교체한다. */
describe("splice", () => {
  it("범위를 새 글로 갈아 끼운다", async () => {
    const { spliceLines } = await import("../src/render/markdown");
    const src = "## 제목\n\n옛 문단\n\n뒤\n";
    expect(spliceLines(src, 2, 3, "새 문단")).toBe("## 제목\n\n새 문단\n\n뒤\n");
  });

  it("여러 줄로 늘어나도 뒤가 안 밀린다", async () => {
    const { spliceLines } = await import("../src/render/markdown");
    const src = "가\n\n나\n\n다\n";
    expect(spliceLines(src, 2, 3, "나1\n나2")).toBe("가\n\n나1\n나2\n\n다\n");
  });

  it("빈 글로 갈면 그 블록이 사라진다", async () => {
    const { spliceLines } = await import("../src/render/markdown");
    expect(spliceLines("가\n\n나\n\n다\n", 2, 3, "")).toBe("가\n\n\n다\n");
  });
});

/** ★ **편집에 들어가도 글의 모양이 안 바뀐다.**
 *
 * 옵시디언은 편집 모드에서도 상자·테두리·글꼴이 그대로다. 보이는 것이 달라지면
 * "지금은 다른 화면" 이라는 느낌이 들고, 그건 우리가 없앤 모드가 되살아나는 것과
 * 같다 (2026-09-01 사업주 지적: "수정 모드의 파란 border 와 스타일을 없애라"). */
describe("편집기 모양", () => {
  const css = readFileSync(resolve(process.cwd(), "src/style.css"), "utf8");
  /** rule 은 그 선택자의 선언 블록을 준다.
   *
   * 정규식으로 잡으면 이스케이프에 물린다 — 규칙을 통째로 쪼개 선택자를 그대로
   * 비교하는 편이 짧고 틀릴 자리가 없다. */
  const rule = (sel: string): string => {
    for (const chunk of css.split("}")) {
      const at = chunk.lastIndexOf("{");
      if (at < 0) continue;
      const head = chunk.slice(0, at).split("\n").pop() ?? "";
      if (head.split(",").map((x) => x.trim()).includes(sel)) return chunk.slice(at + 1);
    }
    return "";
  };

  it("테두리도 배경도 없다", () => {
    const base = rule(".md-editor");
    expect(base, ".md-editor 규칙이 없다").not.toBe("");
    expect(base).toMatch(/border:\s*none/);
    expect(base).toMatch(/background:\s*none/);
  });

  it("본문 글꼴·크기를 그대로 쓴다 — 고정폭으로 바뀌지 않는다", () => {
    const base = rule(".md-editor");
    expect(base).toMatch(/font-family:\s*inherit/);
    expect(base).toMatch(/font-size:\s*var\(--fs-md\)/);
  });

  it("초점을 받아도 외곽선이 안 생긴다", () => {
    expect(css).toMatch(/\.md-editor:focus\s*\{[^}]*outline:\s*none/);
  });

  it("코드 블록만 고정폭을 지킨다 — 렌더된 모습 그대로여야 한다", () => {
    expect(rule(".md-editor--code")).toMatch(/font-family:\s*ui-monospace/);
  });
});
