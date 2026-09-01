import { describe, it, expect, vi, beforeEach } from "vitest";
import type { NoteFull } from "../src/types";

/** ★★★ **고치고 나면 읽던 자리로 돌아와야 한다.**
 *
 * 2026-09-01 재현(사용자 화면 녹화): 긴 결정문을 아래까지 읽다가 한 문단을 눌러
 * 고치고, 마치려고 다른 데를 누르면 **화면이 맨 위로 튄다.** 고친 자리가 어디였는지
 * 다시 찾아야 하고, 길 결정문일수록 더 아프다.
 *
 * 원인은 렌더 구조다. 모든 상태 변화가 `renderBrowse` 를 부르고 그것이 화면을
 * 통째로 다시 만드는데, `.content` 가 **새 요소**라 스크롤이 0에서 시작한다.
 * 저장(blur)은 상태를 두 번 바꾸므로(saving → 완료) 두 번 튄다.
 *
 * 이 파일은 조립부를 실제로 누른다 — 블록을 눌러 편집기를 띄우고, blur 로 저장하고,
 * 그 사이 스크롤을 본다. 렌더 함수만 따로 시험하면 이 고장을 못 잡는다(reader.test.ts
 * 의 검은 화면 사고가 같은 교훈이다). */

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

const ROW = {
  stem: "alpha-결정-저장엔진-2026-08-01", path: "/v/a.md", rel: "a.md", vault: "default",
  domain: ["alpha"], date: "2026-08-01", status: "active", outcome: "pending",
  summary: "저장 엔진을 임베디드 DB 로 고른다", tags: ["decision"],
};
const ROW2 = { ...ROW, stem: "alpha-결정-스키마-2026-08-02", summary: "스키마를 갈랐다" };

// 긴 본문 — 아래까지 스크롤할 것이 있어야 이 고장이 재현된다.
// 끝에 위키링크를 둔다: 글에서 글로 **목록을 거치지 않고** 건너뛰는 길이다.
const BODY = ["## 결정", ""]
  .concat(Array.from({ length: 40 }, (_, i) => `${i} 번째 문단이다.\n`))
  .concat([`이어서 [[${"alpha-결정-스키마-2026-08-02"}]] 을 본다.\n`])
  .join("\n");

function full(row: typeof ROW): NoteFull {
  return {
    ...row, body: BODY, supersedes: [], related: [], author: "LeeJeongHan",
    superseded_reason: "", type: "decision", source_session: "abc", summary_history: [],
  } as NoteFull;
}

function wire(): void {
  invoke.mockImplementation((cmd: string, args?: Record<string, unknown>) => {
    if (cmd === "list_notes") return Promise.resolve(JSON.stringify([ROW, ROW2]));
    if (cmd === "show_note") {
      const stem = args?.stem as string;
      return Promise.resolve(JSON.stringify(full(stem === ROW2.stem ? ROW2 : ROW)));
    }
    if (cmd === "save_body") return Promise.resolve(undefined);
    if (cmd === "queue") return Promise.resolve(JSON.stringify({ confirm: [], review: [], retro: [], health: [] }));
    if (cmd === "settings") return Promise.resolve(JSON.stringify({ vault_parent: "/v", vaults: [], domains: [], hosts: [] }));
    return Promise.resolve(undefined);
  });
}

async function settle(): Promise<void> {
  for (let i = 0; i < 30; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  for (let i = 0; i < 30; i++) await Promise.resolve();
}

async function boot(): Promise<void> {
  document.body.innerHTML = '<div id="app"></div>';
  invoke.mockReset();
  wire();
  vi.resetModules();
  await import("../src/main");
  await settle();
}

function content(): HTMLElement {
  const c = document.querySelector<HTMLElement>(".content");
  expect(c, "본문 칸이 없다").toBeTruthy();
  return c!;
}

function rows(): HTMLButtonElement[] {
  return [...document.querySelectorAll<HTMLButtonElement>(".list-row")];
}

async function openFirstNote(): Promise<void> {
  rows()[0].click();
  await settle();
}

/** blocks 는 눌러서 고칠 수 있는 본문 블록이다. */
function blocks(): HTMLElement[] {
  return [...document.querySelectorAll<HTMLElement>(".md-block")];
}

describe("읽던 자리", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("블록을 눌러 편집기를 띄워도 스크롤이 안 튄다", async () => {
    await boot();
    await openFirstNote();
    content().scrollTop = 420;

    blocks()[6].click();
    await settle();

    expect(document.querySelector(".md-editor"), "편집기가 안 떴다").toBeTruthy();
    expect(content().scrollTop).toBe(420);
  });

  it("★ blur 로 저장을 마쳐도 스크롤이 안 튄다 (녹화된 고장)", async () => {
    await boot();
    await openFirstNote();
    content().scrollTop = 420;

    blocks()[6].click();
    await settle();
    const ta = document.querySelector<HTMLTextAreaElement>(".md-editor")!;
    ta.value = "고친 문단이다.";
    ta.dispatchEvent(new Event("input"));
    ta.dispatchEvent(new Event("blur"));
    await settle();

    expect(document.querySelector(".md-editor"), "편집기가 안 닫혔다").toBeFalsy();
    expect(content().scrollTop, "저장 뒤 맨 위로 튀었다").toBe(420);
  });

  it("Esc 로 물러도 스크롤이 안 튄다", async () => {
    await boot();
    await openFirstNote();
    content().scrollTop = 420;

    blocks()[6].click();
    await settle();
    const ta = document.querySelector<HTMLTextAreaElement>(".md-editor")!;
    ta.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await settle();

    expect(content().scrollTop).toBe(420);
  });

  // ★ 반대쪽도 잡는다. **다른 글을 열면 맨 위에서 시작해야 한다** — 스크롤을
  // 무조건 지키면 새 글이 중간부터 보이고, 그건 원래 고장보다 더 헷갈린다.
  it("다른 결정을 열면 맨 위에서 시작한다", async () => {
    await boot();
    await openFirstNote();
    content().scrollTop = 420;

    // 브레드크럼으로 목록에 돌아갔다가 두 번째 결정을 연다.
    document.querySelector<HTMLButtonElement>(".crumb-back, .crumb button")?.click();
    await settle();
    const r = rows();
    expect(r.length, "목록으로 안 돌아갔다").toBeGreaterThan(1);
    r[1].click();
    await settle();

    expect(content().scrollTop, "새 글이 중간부터 보인다").toBe(0);
  });

  // ★ 위키링크는 **목록을 안 거친다.** 그 길에서는 새 글이 도착할 때까지 옛 글이
  // 잠깐 그대로 그려지는데(main.ts 의 pickNote 는 open 을 안 비운다), 그 사이에
  // 스크롤을 지켜도 **마지막에는 맨 위여야 한다.**
  it("본문의 위키링크로 건너뛰어도 맨 위에서 시작한다", async () => {
    await boot();
    await openFirstNote();
    content().scrollTop = 420;

    const link = [...document.querySelectorAll<HTMLButtonElement>(".md-wikilink, .content button")]
      .find((b) => b.textContent?.includes("스키마"));
    expect(link, "본문에 위키링크가 없다").toBeTruthy();
    link!.click();
    await settle();

    expect(document.querySelector(".reader-title")?.textContent ?? document.body.textContent)
      .toContain("스키마");
    expect(content().scrollTop, "건너뛴 글이 중간부터 보인다").toBe(0);
  });
});
