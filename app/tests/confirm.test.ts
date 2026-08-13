import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderConfirm } from "../src/render/confirm";
import type { Confirm } from "../src/types";

const base: Confirm = {
  id: "/t.jsonl@0",
  domain: "priorcase",
  vault: "personal",
  when: "2026-08-11",
  signals: ["결정", "선택"],
  excerpt: "에이전트: 저장 엔진을 SQLite 로 정했다.",
  fails: 0,
  gave_up: false,
  similar: [],
};

let root: HTMLElement;
const actions = { resolve: vi.fn(), promote: vi.fn() };
// 중괄호를 뺴면 마지막 식의 값이 반환돼 vitest 가 그것을 정리 콜백으로 볼 수
// 있다 (§ priorcase-결정-vitest-beforeEach-반환값이-정리콜백-2026-08-13).
beforeEach(() => {
  root = document.createElement("div");
  actions.resolve.mockClear();
  actions.promote.mockClear();
});

describe("renderConfirm", () => {
  it("비었으면 빈 상태를 그린다", () => {
    renderConfirm(root, [], actions);
    expect(root.textContent).toContain("확인할 구간");
  });

  it("도메인과 볼트를 같이 보여 준다", () => {
    renderConfirm(root, [base], actions);
    expect(root.textContent).toContain("priorcase · personal 볼트");
  });

  // ★★ 볼트를 모르면 **기록 자체가 실패한다** (capture 가 없는 볼트를 거부한다).
  //    여기가 사람이 "결정이다" 를 누르는 자리이므로 반드시 드러내야 한다.
  it("볼트를 모르면 경고를 단다", () => {
    renderConfirm(root, [{ ...base, vault: "" }], actions);
    expect(root.textContent).toContain("볼트 미상");
  });

  // ★★ 포기한 줄은 자동으로 다시 안 온다. 기다리라고 그리면 사람은 영영 기다린다.
  it("포기한 줄은 그렇다고 말한다", () => {
    renderConfirm(root, [{ ...base, fails: 3, gave_up: true }], actions);
    expect(root.textContent).toContain("자동 처리 포기");
    expect(root.textContent).toContain("3");
  });

  it("실패했지만 아직 포기 전이면 횟수만 보여 준다", () => {
    renderConfirm(root, [{ ...base, fails: 2, gave_up: false }], actions);
    expect(root.textContent).toContain("2");
    expect(root.textContent).not.toContain("포기");
  });

  // ★★ 비슷한 결정은 **점수와 함께** 보여 주되 단정하면 안 된다.
  //    회수는 언제나 무언가를 돌려주므로 일치가 없어도 1위가 나온다.
  it("비슷한 결정을 점수와 함께 보여 준다", () => {
    renderConfirm(
      root,
      [
        {
          ...base,
          similar: [
            { stem: "priorcase-결정-저장엔진-2026-08-01", path: "p", summary: "s", score: 118 },
          ],
        },
      ],
      actions,
    );
    expect(root.textContent).toContain("118");
    expect(root.textContent).toContain("저장엔진");
    // 단정 금지
    expect(root.textContent).not.toContain("이미 기록");
    expect(root.textContent).not.toContain("중복");
  });

  it("시그널이 없어도 터지지 않는다", () => {
    // 판별기가 있으면 시그널 필터를 건너뛴다(D9) — 비는 것이 정상이다.
    renderConfirm(root, [{ ...base, signals: [] }], actions);
    expect(root.textContent).toContain("priorcase");
  });

  it("버튼이 콜백을 부른다", () => {
    renderConfirm(root, [base], actions);
    const btns = root.querySelectorAll("button");
    const 결정 = Array.from(btns).find((b) => b.textContent?.includes("결정이다"))!;
    const 아니다 = Array.from(btns).find((b) => b.textContent?.includes("아니다"))!;
    결정.click();
    expect(actions.promote).toHaveBeenCalledWith("/t.jsonl@0");
    아니다.click();
    expect(actions.resolve).toHaveBeenCalledWith("/t.jsonl@0");
  });

  // ★★ **여러 장일 때 각 버튼이 자기 id 를 넘겨야 한다.**
  //
  // 확인 큐는 실측 23건이다. 닫힘(closure)을 잘못 잡으면 어느 줄을 눌러도 마지막
  // 것이 처리되고, 사람은 **엉뚱한 구간을 지운다.** 되돌릴 수 없다.
  it("여러 장이면 각자 자기 id 를 넘긴다", () => {
    renderConfirm(
      root,
      [
        { ...base, id: "/a.jsonl@1" },
        { ...base, id: "/b.jsonl@2" },
        { ...base, id: "/c.jsonl@3" },
      ],
      actions,
    );
    const cards = root.querySelectorAll(".card");
    expect(cards.length).toBe(3);
    const second = cards[1].querySelectorAll("button");
    Array.from(second).find((b) => b.textContent?.includes("아니다"))!.click();
    expect(actions.resolve).toHaveBeenCalledWith("/b.jsonl@2");
    expect(actions.resolve).toHaveBeenCalledTimes(1);

    // **기록 쪽도 같이 본다.** 지우기만 확인하면 "결정이다" 의 닫힘 오류가
    // 그대로 통과한다 — 그건 엉뚱한 구간을 **볼트에 남기는** 쪽이라 더 나쁘다.
    // (실제로 변형 시험에서 이 구멍이 잡혔다.)
    const first = cards[0].querySelectorAll("button");
    Array.from(first).find((b) => b.textContent?.includes("결정이다"))!.click();
    expect(actions.promote).toHaveBeenCalledWith("/a.jsonl@1");
    expect(actions.promote).toHaveBeenCalledTimes(1);
  });

  // ★★ **30초마다 다시 그린다.** 안 지우면 카드가 끝없이 쌓인다.
  it("다시 그려도 쌓이지 않는다", () => {
    for (let i = 0; i < 4; i++) renderConfirm(root, [base], actions);
    expect(root.querySelectorAll(".card").length).toBe(1);
  });

  // ★★ **발췌는 남의 글이다.** 대화 원문이 그대로 들어오고 거기엔 무엇이든 있다.
  it("발췌의 마크업이 그대로 글자로 나온다", () => {
    renderConfirm(root, [{ ...base, excerpt: "<script>x</script><b>굵게</b>" }], actions);
    expect(root.querySelector("b")).toBeNull();
    expect(root.textContent).toContain("<b>굵게</b>");
  });

  // ★★ 발췌만이 남의 글이 아니다. **요약 · stem · 시그널도 전부 남이 쓴 것**이고
  //    이것들은 발췌와 다른 경로(el 의 텍스트 인자)로 화면에 들어간다. 한쪽만
  //    막으면 다른 쪽이 뚫린 채로 남는다 — 변형 시험에서 실제로 그랬다.
  it("요약과 시그널의 마크업도 글자로 나온다", () => {
    renderConfirm(
      root,
      [
        {
          ...base,
          signals: ["<i>결정</i>"],
          similar: [{ stem: "<b>stem</b>", path: "p", summary: "<b>요약</b>", score: 7 }],
        },
      ],
      actions,
    );
    expect(root.querySelector("b")).toBeNull();
    expect(root.querySelector("i")).toBeNull();
    expect(root.textContent).toContain("<b>요약</b>");
  });
});

// ★★★ 발췌 펼치기 — **이 화면이 쓸모 있으려면 근거를 다 볼 수 있어야 한다.**
//
// 실측(2026-08-13, 확인 23건): 발췌가 29~113줄이고 **23건 전부** 8줄을 넘는다.
// 즉 접은 채로만 두면 매 건마다 내용의 72~93% 가 사람에게 영영 안 보인다.
// 그 상태로 "결정이다" 를 누르는 것은 판별기를 믿는 것이지 확인이 아니다.
//
// 그렇다고 전부 펼쳐 두면 23장 × 29~113줄이 한 화면에 쏟아져 훑을 수가 없다.
// 접어 두고 **펼칠 수 있게** 한다.
describe("발췌 펼치기", () => {
  const long = Array.from({ length: 40 }, (_, i) => `줄${i}`).join("\n");

  it("길면 접고 몇 줄이 숨었는지 말한다", () => {
    renderConfirm(root, [{ ...base, excerpt: long }], actions);
    expect(root.querySelector(".excerpt")!.textContent!.split("\n").length).toBe(8);
    expect(root.textContent).toContain("32");
  });

  it("펼치면 전문이 보인다", () => {
    renderConfirm(root, [{ ...base, excerpt: long }], actions);
    const more = root.querySelector(".excerpt-more") as HTMLElement;
    expect(more, "펼칠 수 있는 자리가 있어야 한다").not.toBeNull();
    more.click();
    expect(root.querySelector(".excerpt")!.textContent!.split("\n").length).toBe(40);
    expect(root.querySelector(".excerpt")!.textContent).toContain("줄39");
  });

  it("펼친 뒤 다시 접을 수 있다", () => {
    renderConfirm(root, [{ ...base, excerpt: long }], actions);
    const more = root.querySelector(".excerpt-more") as HTMLElement;
    more.click();
    (root.querySelector(".excerpt-more") as HTMLElement).click();
    expect(root.querySelector(".excerpt")!.textContent!.split("\n").length).toBe(8);
  });

  it("짧으면 펼칠 자리를 안 만든다", () => {
    renderConfirm(root, [base], actions);
    expect(root.querySelector(".excerpt-more")).toBeNull();
  });

  // ★★ 펼치기는 **그 장만** 펼쳐야 한다. 하나 눌렀는데 23장이 다 펼쳐지면
  //    훑기가 불가능해진다.
  it("한 장을 펼쳐도 다른 장은 접힌 채다", () => {
    renderConfirm(
      root,
      [
        { ...base, id: "/a@1", excerpt: long },
        { ...base, id: "/b@2", excerpt: long },
      ],
      actions,
    );
    const cards = root.querySelectorAll(".card");
    (cards[0].querySelector(".excerpt-more") as HTMLElement).click();
    expect(cards[0].querySelector(".excerpt")!.textContent!.split("\n").length).toBe(40);
    expect(cards[1].querySelector(".excerpt")!.textContent!.split("\n").length).toBe(8);
  });
});
