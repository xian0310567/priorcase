import { describe, it, expect, vi } from "vitest";
import { markdown } from "../src/render/markdown";

/** ★ 지원 범위는 **실볼트를 재서** 정했다 (2026-09-01, 결정 575건):
 *
 *   제목 85% · 인라인코드 65% · 굵게 63% · 목록 61% · 중첩목록 57% ·
 *   위키링크 35% · 표 33% · 번호목록 23% · 코드블록 13% · 인용 8%
 *
 * 아래 셋이 예전 렌더러가 못 그리던 것이고, 절반 넘는 노트가 그중 하나를 쓴다. */
describe("마크다운", () => {
  it("중첩 목록의 층위를 살린다 — 57% 가 쓴다", () => {
    const md = markdown("- 바깥\n  - 안쪽\n  - 안쪽 둘\n- 바깥 둘\n").root;
    const top = md.querySelector(":scope > ul")!;
    expect(top.children.length, "바깥 항목이 둘이어야 한다").toBe(2);
    expect(top.querySelector("ul.md-sublist")!.children.length).toBe(2);
  });

  it("표를 진짜 표로 그린다 — 33% 가 쓰고 대부분 실측 표다", () => {
    const md = markdown("| 안 | 결과 |\n| --- | --- |\n| A | 2.6초 |\n| B | 32초 |\n").root;
    const t = md.querySelector("table.md-table")!;
    expect([...t.querySelectorAll("th")].map((x) => x.textContent)).toEqual(["안", "결과"]);
    expect(t.querySelectorAll("tbody tr").length).toBe(2);
    expect(t.querySelectorAll("tbody tr")[1].textContent).toContain("32초");
  });

  it("위키링크를 누를 수 있다 — 그래야 멘션이다", () => {
    const onLink = vi.fn();
    const md = markdown("앞선 [[alpha-결정-저장엔진-2026-08-01]] 을 본다\n", { onLink }).root;
    const b = md.querySelector<HTMLButtonElement>("button.md-wiki-link")!;
    expect(b.textContent).toBe("alpha-결정-저장엔진-2026-08-01");
    b.click();
    expect(onLink).toHaveBeenCalledWith("alpha-결정-저장엔진-2026-08-01");
  });

  it("별칭과 절 앵커를 벗겨 stem 으로 연다", () => {
    const onLink = vi.fn();
    const md = markdown("[[stem-a|보이는 글]] 과 [[stem-b#절]]\n", { onLink }).root;
    const bs = md.querySelectorAll<HTMLButtonElement>("button.md-wiki-link");
    expect(bs[0].textContent).toBe("보이는 글");
    bs[0].click();
    expect(onLink).toHaveBeenLastCalledWith("stem-a");
    bs[1].click();
    expect(onLink).toHaveBeenLastCalledWith("stem-b");
  });

  it("번호 목록은 번호를 지킨다", () => {
    expect(markdown("1. 하나\n2. 둘\n").root.querySelector("ol")).toBeTruthy();
  });

  it("문단은 줄바꿈 하나를 공백으로 읽는다", () => {
    const p = markdown("첫 줄\n이어지는 줄\n\n다음 문단\n").root.querySelectorAll("p.md-p");
    expect(p.length).toBe(2);
    expect(p[0].textContent).toBe("첫 줄 이어지는 줄");
  });

  // ★★ **HTML 을 만들지 않는다.** 이 앱은 남이 쓴 볼트(회사 볼트)도 연다 —
  // 결정문 한 줄이 앱을 조작할 수 있으면 안 된다.
  it("본문의 HTML 은 글자로만 들어간다", () => {
    const md = markdown("<img src=x onerror=alert(1)> 그리고 <b>굵게</b>\n").root;
    expect(md.querySelector("img"), "볼트 내용이 요소가 됐다").toBeNull();
    expect(md.querySelector("b"), "볼트 내용이 요소가 됐다").toBeNull();
    expect(md.textContent).toContain("<img src=x onerror=alert(1)>");
  });

  it("코드 블록 안의 문법은 안 해석한다", () => {
    const md = markdown("```bash\n- 목록처럼 보이는 줄\n**굵게 아님**\n```\n").root;
    expect(md.querySelector("pre.md-code code")!.textContent)
      .toBe("- 목록처럼 보이는 줄\n**굵게 아님**");
    expect(md.querySelector("strong")).toBeNull();
  });
});

/** ★ 들여쓴 코드 블록 — **실측 표가 사는 자리다.**
 *
 * 결정문은 측정값을 파이프 표가 아니라 탭으로 들여써 적는 일이 많고, 정렬이 곧
 * 뜻이라 문단으로 뭉개면 숫자가 어느 열인지 알 수 없게 된다. 실볼트의 회수 개선
 * 결정문 하나에만 그런 줄이 13개 있었다. */
describe("들여쓴 블록", () => {
  it("탭으로 들여쓴 표를 코드로 그린다 — 정렬이 뜻이다", () => {
    const src = "잰 값:\n\n\t2026       df 540 (100.0%)\n\t08         df 538 ( 99.6%)\n\n다음 문단\n";
    const md = markdown(src).root;
    const pre = md.querySelector("pre.md-code")!;
    expect(pre.textContent).toBe("2026       df 540 (100.0%)\n08         df 538 ( 99.6%)");
    expect(md.querySelectorAll("p.md-p").length).toBe(2);
  });

  it("중첩 목록을 코드로 오독하지 않는다", () => {
    const md = markdown("- 바깥\n  - 안쪽\n").root;
    expect(md.querySelector("pre"), "목록이 코드가 됐다").toBeNull();
    expect(md.querySelector("ul.md-sublist")).toBeTruthy();
  });

  it("문단 바로 아래 들여쓴 줄은 코드가 아니다 — 이어지는 글이다", () => {
    const md = markdown("문단이다\n    이어지는 줄\n").root;
    expect(md.querySelector("pre")).toBeNull();
  });
});
