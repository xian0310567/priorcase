import { describe, it, expect, beforeEach, vi } from "vitest";
import { renderReview, stemOf } from "../src/render/review";
import type { Review } from "../src/types";

const base: Review = {
  id: "/t.jsonl@0",
  domain: "priorcase",
  vault: "personal",
  at: "2026-08-12T10:00:00Z",
  path: "priorcase/decisions/priorcase-결정-저장엔진-2026-08-12.md",
  excerpt: "사용자: SQLite 로 가자\n에이전트: 확인했습니다",
};

let root: HTMLElement;
const actions = { ok: vi.fn(), open: vi.fn() };
beforeEach(() => {
  root = document.createElement("div");
  actions.ok.mockClear();
  actions.open.mockClear();
});

describe("stemOf", () => {
  it("볼트 상대 경로에서 stem 을 뽑는다", () => {
    expect(stemOf("priorcase/decisions/priorcase-결정-저장엔진-2026-08-12.md")).toBe(
      "priorcase-결정-저장엔진-2026-08-12",
    );
  });

  // ★ 볼트 폴더 이름에 점이 있다 (실측: draft.ai/decisions/…).
  it("폴더 이름의 점에 안 걸린다", () => {
    expect(stemOf("draft.ai/decisions/draft00-결정-로케일-2026-08-10.md")).toBe(
      "draft00-결정-로케일-2026-08-10",
    );
  });

  // ★★ **stem 자체에도 점이 들어올 수 있다.**
  //
  // store.Slugify 의 거부목록은 `/\:*?"<>|` 이고 **점은 거기 없다** — 즉
  // `prior capture --slug "v1.2 마이그레이션"` 은 `v1.2-마이그레이션` 이 된다.
  // 마지막 점(또는 첫 점)부터 자르면 stem 이 `…-결정-v1` 로 잘리고, prior path
  // 가 "그런 노트가 없다" 로 거절해 **노트 열기가 조용히 안 먹는다.**
  //
  // 볼트에 아직 그런 노트가 없다는 것은 안전의 근거가 아니다 — 막는 것이 없다.
  it("stem 에 점이 있어도 .md 만 뗀다", () => {
    expect(stemOf("priorcase/decisions/priorcase-결정-v1.2-마이그레이션-2026-08-13.md")).toBe(
      "priorcase-결정-v1.2-마이그레이션-2026-08-13",
    );
  });
});

describe("renderReview", () => {
  it("비었으면 빈 상태를 그린다", () => {
    renderReview(root, [], actions);
    expect(root.textContent).toContain("검토할 노트");
  });

  it("발췌와 노트 경로를 나란히 놓는다", () => {
    renderReview(root, [base], actions);
    expect(root.textContent).toContain("SQLite 로 가자");
    expect(root.textContent).toContain("priorcase-결정-저장엔진-2026-08-12");
  });

  it("도메인과 볼트를 같이 보여 준다", () => {
    renderReview(root, [base], actions);
    expect(root.textContent).toContain("priorcase · personal 볼트");
  });

  // ★★★ 발췌가 없으면 **없다고 말해야 한다.**
  //     조용히 안 보여 주면 사람은 노트만 보고 맞다고 누른다 — 그게 이 화면의
  //     존재 이유(판별기 날조 검증)를 통째로 무너뜨린다.
  //     실측(2026-08-13): 지금 검토 큐 3건이 **전부** 발췌가 없다 (옛 원장).
  it("대조할 발췌가 없으면 그렇다고 말한다", () => {
    renderReview(root, [{ ...base, excerpt: "" }], actions);
    expect(root.textContent).toContain("대조할 발췌가 없다");
  });

  // ★★ **발췌가 없을 때 "맞다" 를 누르게 두면 안 된다.**
  //
  // 대조할 것이 없는데 "판별기가 사실대로 썼다" 고 표시하는 것은 검증이 아니라
  // 서명이다. 노트를 열어 본 뒤에야 판단할 수 있으므로, 그때는 열기만 남긴다.
  it("발췌가 없으면 맞다 버튼을 막는다", () => {
    renderReview(root, [{ ...base, excerpt: "" }], actions);
    const ok = Array.from(root.querySelectorAll("button")).find((b) =>
      b.textContent?.includes("맞다"),
    ) as HTMLButtonElement | undefined;
    expect(ok?.disabled, "발췌가 없으면 눌리면 안 된다").toBe(true);
    ok?.click();
    expect(actions.ok).not.toHaveBeenCalled();
  });

  // ★★★ **"맞다" 는 승격 ID 로 부른다 — stem 이 아니다.**
  //
  // 검토 표시는 승격 원장에 남고 그 키가 ID 다. stem 을 넘기면 prior reviewed
  // 가 "그런 기록이 없다" 로 거절한다.
  it("맞다 버튼이 승격 ID 로 콜백을 부른다", () => {
    renderReview(root, [base], actions);
    const btn = Array.from(root.querySelectorAll("button")).find((b) =>
      b.textContent?.includes("맞다"),
    )!;
    btn.click();
    expect(actions.ok).toHaveBeenCalledWith("/t.jsonl@0");
  });

  // ★ 앱이 볼트 경로를 조립하지 않으려면 stem 을 넘겨야 한다 —
  //   prior path <stem> 이 경로를 푼다.
  it("노트 열기 버튼이 stem 으로 콜백을 부른다", () => {
    renderReview(root, [base], actions);
    const btn = Array.from(root.querySelectorAll("button")).find((b) =>
      b.textContent?.includes("노트 열기"),
    )!;
    btn.click();
    expect(actions.open).toHaveBeenCalledWith("priorcase-결정-저장엔진-2026-08-12");
  });

  // ★★ 여러 장일 때 각자 자기 것을 넘겨야 한다. 검증 표시는 되돌리기 어렵다.
  it("여러 장이면 각자 자기 것을 넘긴다", () => {
    renderReview(
      root,
      [
        { ...base, id: "/a@1", path: "alpha/decisions/alpha-결정-가-2026-08-01.md" },
        { ...base, id: "/b@2", path: "beta/decisions/beta-결정-나-2026-08-02.md" },
      ],
      actions,
    );
    const cards = root.querySelectorAll(".card");
    expect(cards.length).toBe(2);
    Array.from(cards[1].querySelectorAll("button"))
      .find((b) => b.textContent?.includes("맞다"))!
      .click();
    expect(actions.ok).toHaveBeenCalledWith("/b@2");
    expect(actions.ok).toHaveBeenCalledTimes(1);

    Array.from(cards[0].querySelectorAll("button"))
      .find((b) => b.textContent?.includes("노트 열기"))!
      .click();
    expect(actions.open).toHaveBeenCalledWith("alpha-결정-가-2026-08-01");
  });

  // ★★ 30초마다 다시 그린다.
  it("다시 그려도 쌓이지 않는다", () => {
    for (let i = 0; i < 4; i++) renderReview(root, [base], actions);
    expect(root.querySelectorAll(".card").length).toBe(1);
  });

  // ★★ 발췌도 노트 경로도 **남의 글**이다.
  it("마크업이 그대로 글자로 나온다", () => {
    renderReview(
      root,
      [{ ...base, excerpt: "<b>굵게</b>", path: "x/decisions/<i>stem</i>.md" }],
      actions,
    );
    expect(root.querySelector("b")).toBeNull();
    expect(root.querySelector("i")).toBeNull();
    expect(root.textContent).toContain("<b>굵게</b>");
  });
});

// ★★ 검토 발췌도 길다 — 확인 큐와 같은 이유로 펼칠 수 있어야 한다.
describe("검토 발췌 펼치기", () => {
  const long = Array.from({ length: 40 }, (_, i) => `줄${i}`).join("\n");

  it("길면 접고 펼칠 수 있다", () => {
    renderReview(root, [{ ...base, excerpt: long }], actions);
    expect(root.querySelector(".excerpt")!.textContent!.split("\n").length).toBe(12);
    const more = root.querySelector(".excerpt-more") as HTMLElement;
    expect(more, "펼칠 자리가 있어야 한다").not.toBeNull();
    more.click();
    expect(root.querySelector(".excerpt")!.textContent!.split("\n").length).toBe(40);
  });

  it("짧으면 펼칠 자리를 안 만든다", () => {
    renderReview(root, [base], actions);
    expect(root.querySelector(".excerpt-more")).toBeNull();
  });
});
