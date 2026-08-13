import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderRetro, resetDismissed } from "../src/render/retro";
import type { Retro } from "../src/types";

const base: Retro = {
  stem: "priorcase-결정-저장엔진-2026-08-01",
  date: "2026-08-01",
  domain: "priorcase",
  vault: "personal",
  summary: "저장 엔진을 임베디드 DB 로 고른다",
  author: "zesty <x@y.z>",
  reason: "recalled",
  hits: 4,
};

let root: HTMLElement;
const actions = { judge: vi.fn(), open: vi.fn() };
beforeEach(() => {
  root = document.createElement("div");
  actions.judge.mockClear();
  actions.open.mockClear();
  resetDismissed();
});

describe("renderRetro", () => {
  it("비었으면 빈 상태를 그린다", () => {
    renderRetro(root, [], actions);
    expect(root.textContent).toContain("회고할 결정");
  });

  it("재회수 횟수를 보여 준다", () => {
    renderRetro(root, [base], actions);
    expect(root.textContent).toContain("재회수 4회");
    expect(root.textContent).toContain("저장 엔진을 임베디드 DB 로 고른다");
  });

  // ★ superseded 는 hits 가 0 일 수 있다 — "재회수 0회" 는 거짓이다.
  //   실측(2026-08-13): 회고 50건 중 9건이 superseded 이고 7건이 hits 0 이다.
  it("뒤집힌 것은 횟수를 말하지 않는다", () => {
    renderRetro(root, [{ ...base, reason: "superseded", hits: 0 }], actions);
    expect(root.textContent).toContain("뒤집혔다");
    expect(root.textContent).not.toContain("0회");
  });

  it("좋았다/나빴다가 콜백을 부른다", () => {
    renderRetro(root, [base], actions);
    const btns = Array.from(root.querySelectorAll("button"));
    btns.find((b) => b.textContent === "좋았다")!.click();
    expect(actions.judge).toHaveBeenCalledWith(base.stem, "good");
    btns.find((b) => b.textContent === "나빴다")!.click();
    expect(actions.judge).toHaveBeenCalledWith(base.stem, "bad");
  });

  // ★ [아직] 은 명령을 부르지 않는다 — 같은 방아쇠가 한 번 더 울릴 때까지
  //   안 묻는 것이 규칙이고, 그건 재회수 카운트가 오르면 자연히 된다.
  it("아직 버튼은 아무 명령도 안 부른다", () => {
    renderRetro(root, [base], actions);
    Array.from(root.querySelectorAll("button")).find((b) => b.textContent === "아직")!.click();
    expect(actions.judge).not.toHaveBeenCalled();
  });

  // ★★★ **[아직] 이 다시 그려도 유지돼야 한다.**
  //
  // 앱은 30초마다 큐를 다시 받아 다시 그린다. 카드를 DOM 에서만 지우면
  // **30초 뒤 그대로 돌아온다** — 실측으로 회고 큐가 50건이라, 미룬 것이 계속
  // 되살아나면 사람은 그 화면을 통째로 포기한다.
  //
  // 미룸은 **앱이 떠 있는 동안만** 기억한다. 파일에 남기면 "왜 안 뜨지" 를
  // 설명할 규칙이 둘이 되고, 되살릴 방법이 없어진다. 앱을 다시 켜면 다시 뜬다.
  it("아직은 다시 그려도 유지된다", () => {
    renderRetro(root, [base], actions);
    Array.from(root.querySelectorAll("button")).find((b) => b.textContent === "아직")!.click();
    expect(root.querySelectorAll(".card").length).toBe(0);

    // 30초 뒤 폴링
    renderRetro(root, [base], actions);
    expect(root.querySelectorAll(".card").length, "미룬 것이 되살아났다").toBe(0);
    expect(root.textContent).toContain("회고할 결정");
  });

  it("미룬 것이 있어도 다른 것은 그린다", () => {
    const other = { ...base, stem: "priorcase-결정-다른것-2026-08-02" };
    renderRetro(root, [base, other], actions);
    const cards = root.querySelectorAll(".card");
    Array.from(cards[0].querySelectorAll("button")).find((b) => b.textContent === "아직")!.click();

    renderRetro(root, [base, other], actions);
    expect(root.querySelectorAll(".card").length).toBe(1);
    expect(root.textContent).toContain("다른것");
    expect(root.textContent).not.toContain("저장엔진");
  });

  // ★★ 여러 장일 때 각자 자기 stem 을 넘겨야 한다. 실측 50건이다.
  it("여러 장이면 각자 자기 stem 을 넘긴다", () => {
    renderRetro(
      root,
      [
        { ...base, stem: "a-결정-가-2026-08-01" },
        { ...base, stem: "b-결정-나-2026-08-02" },
      ],
      actions,
    );
    const cards = root.querySelectorAll(".card");
    Array.from(cards[1].querySelectorAll("button"))
      .find((b) => b.textContent === "좋았다")!
      .click();
    expect(actions.judge).toHaveBeenCalledWith("b-결정-나-2026-08-02", "good");
    expect(actions.judge).toHaveBeenCalledTimes(1);
  });

  it("다시 그려도 쌓이지 않는다", () => {
    for (let i = 0; i < 4; i++) renderRetro(root, [base], actions);
    expect(root.querySelectorAll(".card").length).toBe(1);
  });

  // ★ author 는 대개 없다 (실측 50건 중 8건만 있다). 없어도 빈 줄을 안 만든다.
  it("author 가 없으면 그 줄을 안 만든다", () => {
    const { author: _drop, ...noAuthor } = base;
    renderRetro(root, [noAuthor as Retro], actions);
    expect(root.querySelector(".author")).toBeNull();
  });

  // ★★ **어느 노트인지 보여야 한다.** 요약만으로는 비슷한 결정 둘을 구별할 수
  //    없다 — 실측으로 회고 큐가 50건이고 priorcase 도메인만 24건이다.
  it("어느 노트인지 stem 을 보여 준다", () => {
    renderRetro(root, [base], actions);
    expect(root.textContent).toContain("priorcase-결정-저장엔진-2026-08-01");
  });

  // ★★ **열어 볼 수 있어야 한다.** 요약 한 줄(실측 30~139자)로 "결과적으로
  //    좋았나" 를 판정하는 것은 확인이 아니라 짐작이다.
  it("노트 열기가 stem 으로 콜백을 부른다", () => {
    renderRetro(root, [base], actions);
    Array.from(root.querySelectorAll("button"))
      .find((b) => b.textContent === "노트 열기")!
      .click();
    expect(actions.open).toHaveBeenCalledWith(base.stem);
  });

  it("볼트를 모르면 그렇다고 말한다", () => {
    renderRetro(root, [{ ...base, vault: "" }], actions);
    expect(root.textContent).toContain("볼트 미상");
  });

  // ★★ 요약도 stem 도 남의 글이다.
  it("마크업이 그대로 글자로 나온다", () => {
    renderRetro(root, [{ ...base, summary: "<b>굵게</b>" }], actions);
    expect(root.querySelector("b")).toBeNull();
    expect(root.textContent).toContain("<b>굵게</b>");
  });
});
