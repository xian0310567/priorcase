import { describe, it, expect, beforeAll } from "vitest";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { renderConfirm } from "../src/render/confirm";
import { renderReview } from "../src/render/review";
import { renderRetro, resetDismissed } from "../src/render/retro";
import { renderHealth } from "../src/render/health";
import { renderWarnings } from "../src/render/shell";
import type { Queue } from "../src/types";

// ★★★ **진짜 볼트의 큐를 화면 함수에 통과시킨다.**
//
// 합성 픽스처는 내가 상상한 모양만 담는다. 실제 데이터는 다르다 — 실측으로
// 발췌가 29~113줄이고, 검토 3건이 전부 발췌가 없고, 회고가 50건이다. 그런
// 모양은 픽스처를 쓸 때 떠올리지 못한 것들이고, **호스트 파서에서도 합성
// 픽스처가 못 잡은 결함 둘을 실측 데이터가 잡은 전력이 있다.**
//
// prior 가 없거나 볼트가 없는 기계에서는 건너뛴다 — 이 시험은 여기 있는 것이
// 값이지, 어디서나 도는 것이 값이 아니다.

const PRIOR = join(homedir(), ".local", "bin", "prior");
const available = existsSync(PRIOR);

let q: Queue;
beforeAll(() => {
  if (!available) return;
  const raw = execFileSync(PRIOR, ["queue", "--json"], {
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  q = JSON.parse(raw) as Queue;
});

describe.skipIf(!available)("진짜 큐로 화면을 그린다", () => {
  it("큐가 계약을 지킨다", () => {
    for (const k of ["confirm", "review", "retro", "health"] as const) {
      expect(Array.isArray(q[k]), `${k} 가 배열이 아니다`).toBe(true);
    }
    // 줄마다 볼트가 붙어야 한다 — 앱에는 cwd 가 없다.
    for (const c of q.confirm) expect(c.vault, `확인 ${c.id}: 볼트가 없다`).not.toBe("");
    for (const r of q.review) expect(r.vault, `검토 ${r.id}: 볼트가 없다`).not.toBe("");
    for (const r of q.retro) expect(r.vault, `회고 ${r.stem}: 볼트가 없다`).not.toBe("");
    // 안쪽 배열도 null 이 아니어야 한다 — null.map 은 화면을 통째로 죽인다.
    for (const c of q.confirm) {
      expect(Array.isArray(c.signals), `확인 ${c.id}: signals 가 배열이 아니다`).toBe(true);
      expect(Array.isArray(c.similar), `확인 ${c.id}: similar 가 배열이 아니다`).toBe(true);
    }
  });

  it("확인 큐가 그려진다", () => {
    const root = document.createElement("div");
    renderConfirm(root, q.confirm, { resolve: () => {}, promote: () => {} });
    expect(root.querySelectorAll(".card").length).toBe(q.confirm.length);
    if (q.confirm.length > 0) {
      // 접히지 않은 카드가 하나도 없으면 접기 상수가 무의미하다는 뜻이다.
      // 실측으로 발췌가 29~113줄이므로 대부분 접혀야 한다.
      const folded = root.querySelectorAll(".excerpt-more").length;
      expect(folded, "긴 발췌인데 접힌 것이 하나도 없다").toBeGreaterThan(0);
    }
  });

  it("검토 큐가 그려진다", () => {
    const root = document.createElement("div");
    renderReview(root, q.review, { ok: () => {}, open: () => {} });
    expect(root.querySelectorAll(".card").length).toBe(q.review.length);
    // 발췌가 없는 줄은 [맞다] 가 막혀 있어야 한다.
    for (const [i, r] of q.review.entries()) {
      if (r.excerpt.trim() !== "") continue;
      const ok = root
        .querySelectorAll(".card")
        [i].querySelector<HTMLButtonElement>("button.primary")!;
      expect(ok.disabled, `검토 ${r.id}: 발췌가 없는데 맞다가 눌린다`).toBe(true);
    }
  });

  it("회고 큐가 그려진다", () => {
    resetDismissed();
    const root = document.createElement("div");
    renderRetro(root, q.retro, { judge: () => {}, open: () => {} });
    expect(root.querySelectorAll(".card").length).toBe(q.retro.length);
    // superseded 인 줄에 "재회수 0회" 가 뜨면 거짓말이다.
    if (q.retro.some((r) => r.reason === "superseded" && r.hits === 0)) {
      expect(root.textContent).not.toContain("재회수 0회");
    }
  });

  it("상태가 그려지고 모르는 등급이 없다", () => {
    const root = document.createElement("div");
    renderHealth(root, q.health);
    expect(root.querySelectorAll(".health-row").length).toBe(q.health.length);
    // 실제 prior 가 내는 등급은 계약 안에 있어야 한다. unknown 이 나오면
    // Go 쪽에 등급이 늘었는데 타입을 안 고친 것이다.
    expect(root.querySelectorAll(".level-unknown").length, "모르는 등급이 왔다").toBe(0);
  });

  it("경고가 있으면 배너가 뜬다", () => {
    const root = document.createElement("div");
    renderWarnings(root, q.warnings);
    if (q.warnings && q.warnings.length > 0) {
      expect(root.textContent).toContain("불완전");
    } else {
      expect(root.innerHTML).toBe("");
    }
  });

  // ★★ **남의 글이 화면 구조를 못 뒤튼다.** 실제 발췌에는 대화 원문이 통째로
  //    들어 있고 거기엔 코드 블록·태그·따옴표가 섞인다.
  it("실제 발췌가 요소를 만들지 않는다", () => {
    const root = document.createElement("div");
    renderConfirm(root, q.confirm, { resolve: () => {}, promote: () => {} });
    // 우리가 만드는 태그만 있어야 한다.
    const allowed = new Set(["DIV", "PRE", "SPAN", "BUTTON", "P"]);
    const bad = Array.from(root.querySelectorAll("*")).filter(
      (n) => !allowed.has(n.tagName),
    );
    expect(bad.map((n) => n.tagName), "발췌에서 온 태그가 있다").toEqual([]);
  });
});
