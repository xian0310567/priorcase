import { describe, it, expect, vi, beforeAll } from "vitest";
import type { Confirm, Retro, Review } from "../src/types";

// ★★★ **배선을 화면에서 눌러 확인한다.**
//
// 이 세션에서만 "필드·함수는 있는데 조립부가 안 붙였다" 가 다섯 번 나왔다
// (similarFor · sweepOthers · 데몬 정리 · 확인 큐 볼트 · 검토 큐 볼트).
// 공통점이 분명하다 — **손으로 만든 객체로 하는 시험은 조립부를 안 지나간다.**
//
// 여기서는 진짜 #app 에 앱을 띄우고, 진짜 버튼을 눌러, **어떤 IPC 가 나가는지**를
// 본다. 화면 함수가 옳아도 main.ts 가 엉뚱한 명령에 연결하면 여기서 잡힌다.
// 특히 검토 큐의 [맞다] 는 review(outcome) 가 아니라 mark_reviewed 여야 한다 —
// 잘못 연결하면 회고 큐가 조용히 망가진다.

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

const confirm1: Confirm = {
  id: "/t.jsonl@1",
  domain: "priorcase",
  vault: "personal",
  when: "2026-08-11",
  signals: [],
  excerpt: "저장 엔진을 정했다",
  fails: 0,
  gave_up: false,
  similar: [],
};
const review1: Review = {
  id: "/t.jsonl@2",
  domain: "priorcase",
  vault: "personal",
  at: "2026-08-12T10:00:00Z",
  path: "priorcase/decisions/priorcase-결정-저장엔진-2026-08-12.md",
  excerpt: "판별기가 본 것",
};
const retro1: Retro = {
  stem: "priorcase-결정-옛것-2026-08-01",
  date: "2026-08-01",
  domain: "priorcase",
  vault: "personal",
  summary: "요약",
  reason: "recalled",
  hits: 3,
};

const QUEUE = JSON.stringify({
  confirm: [confirm1],
  review: [review1],
  retro: [retro1],
  health: [{ name: "볼트", level: "ok", detail: "/a" }],
});

/** 큐를 주는 가짜 invoke. 쓰기 명령은 undefined 를 준다. */
function wire(): void {
  invoke.mockImplementation((cmd: string) => {
    if (cmd === "queue") return Promise.resolve(QUEUE);
    return Promise.resolve(undefined);
  });
}

/** 폴링이 한 바퀴 돌기를 기다린다. */
async function settle(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

function tabButton(label: string): HTMLButtonElement {
  return Array.from(document.querySelectorAll<HTMLButtonElement>(".tabs .tab")).find(
    (b) => b.textContent?.startsWith(label),
  )!;
}

function button(text: string): HTMLButtonElement {
  return Array.from(document.querySelectorAll<HTMLButtonElement>(".body button")).find(
    (b) => b.textContent?.includes(text),
  )!;
}

/** 마지막으로 나간 쓰기 명령. set_tray_title·queue 는 뺀다. */
function lastWrite(): [string, unknown] | undefined {
  const w = invoke.mock.calls.filter(
    (c) => c[0] !== "queue" && c[0] !== "set_tray_title",
  );
  return w.length ? (w[w.length - 1] as [string, unknown]) : undefined;
}

beforeAll(async () => {
  document.body.innerHTML = '<div id="app"></div>';
  wire();
  await import("../src/main");
  await settle();
});

describe("조립", () => {
  it("탭 넷이 뜨고 개수가 붙는다", () => {
    const labels = Array.from(document.querySelectorAll(".tabs .tab")).map(
      (b) => b.textContent,
    );
    expect(labels).toEqual(["확인 1", "검토 1", "회고 1", "상태"]);
  });

  it("트레이 글자를 합계로 갱신한다", () => {
    const calls = invoke.mock.calls.filter((c) => c[0] === "set_tray_title");
    expect(calls.length, "트레이를 한 번도 안 갱신했다").toBeGreaterThan(0);
    expect(calls[calls.length - 1][1]).toEqual({ title: "3" });
  });

  it("확인 큐의 [결정이다] 는 promote 를 부른다", async () => {
    button("결정이다").click();
    await settle();
    expect(lastWrite()).toEqual(["promote", { id: "/t.jsonl@1" }]);
  });

  it("확인 큐의 [아니다] 는 resolve_pending 을 부른다", async () => {
    button("아니다").click();
    await settle();
    expect(lastWrite()).toEqual(["resolve_pending", { id: "/t.jsonl@1" }]);
  });

  // ★★★ **[맞다] 는 mark_reviewed 여야 한다.**
  //
  // review(outcome) 에 연결하면 회고 큐가 그 노트를 영영 제외한다 — 노트를
  // 검증했을 뿐인데 나중에 결과를 묻는 자리가 조용히 사라진다.
  it("검토 큐의 [맞다] 는 mark_reviewed 를 승격 ID 로 부른다", async () => {
    tabButton("검토").click();
    await settle();
    button("맞다").click();
    await settle();
    expect(lastWrite()).toEqual(["mark_reviewed", { id: "/t.jsonl@2" }]);
  });

  it("검토 큐의 [노트 열기] 는 open_note 를 stem 으로 부른다", async () => {
    tabButton("검토").click();
    await settle();
    button("노트 열기").click();
    await settle();
    expect(lastWrite()).toEqual([
      "open_note",
      { stem: "priorcase-결정-저장엔진-2026-08-12" },
    ]);
  });

  it("회고 큐의 [좋았다] 는 review 를 outcome 과 함께 부른다", async () => {
    tabButton("회고").click();
    await settle();
    button("좋았다").click();
    await settle();
    expect(lastWrite()).toEqual([
      "review",
      { stem: "priorcase-결정-옛것-2026-08-01", outcome: "good" },
    ]);
  });

  it("회고 큐의 [나빴다] 는 bad 를 보낸다", async () => {
    tabButton("회고").click();
    await settle();
    button("나빴다").click();
    await settle();
    expect(lastWrite()).toEqual([
      "review",
      { stem: "priorcase-결정-옛것-2026-08-01", outcome: "bad" },
    ]);
  });

  it("상태 탭이 검사를 그린다", async () => {
    tabButton("상태").click();
    await settle();
    expect(document.querySelectorAll(".health-row").length).toBe(1);
  });
});
