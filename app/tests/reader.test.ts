import { describe, it, expect, vi, beforeEach } from "vitest";
import type { NoteFull } from "../src/types";

/** ★★★ **결정을 눌러 본문을 여는 경로를 실제로 누른다.**
 *
 * 2026-09-01 사고: 목록에서 결정을 누르면 **화면이 검게 비었다.** 그때 프런트
 * 테스트가 92개였는데 하나도 이 경로를 안 지났다 — `renderBrowse` 는 `open: null`
 * 로만 불렀고 `markdown()` 은 따로 시험했다. **조립부를 안 지나가는 시험이었다.**
 *
 * 원인은 셋이 겹친 것이다:
 *   ① 앱에 번들된 prior 가 낡아서 `summary_health` 같은 새 키를 안 냈다
 *   ② 속성 패널이 그 키를 무조건 읽어 TypeError 를 냈다
 *   ③ 렌더 중 예외가 화면을 통째로 비웠다 (오류 경계가 없었다)
 *
 * 이 파일은 셋을 각각 잠근다. */

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

const ROW = {
  stem: "alpha-결정-저장엔진-2026-08-01", path: "/v/a.md", rel: "a.md", vault: "default",
  domain: ["alpha"], date: "2026-08-01", status: "active", outcome: "pending",
  summary: "저장 엔진을 임베디드 DB 로 고른다", tags: ["decision", "저장엔진"],
};

const FULL: NoteFull = {
  ...ROW,
  body: "## 결정\n\n임베디드 DB 로 간다.\n\n- 하나\n  - 안쪽\n\n앞선 [[alpha-결정-스키마-2026-08-02]] 을 뒤집는다.\n",
  supersedes: ["[[alpha-결정-스키마-2026-08-02]]"], related: [], author: "LeeJeongHan",
  superseded_reason: "", type: "decision", source_session: "abc", summary_history: [],
};

/** wire 는 show 가 무엇을 줄지 갈아 끼운다 — 판 차이를 흉내 내는 유일한 문이다. */
function wire(showPayload: unknown): void {
  invoke.mockImplementation((cmd: string) => {
    if (cmd === "list_notes") return Promise.resolve(JSON.stringify([ROW]));
    if (cmd === "show_note") return Promise.resolve(JSON.stringify(showPayload));
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

async function boot(showPayload: unknown): Promise<void> {
  document.body.innerHTML = '<div id="app"></div>';
  invoke.mockReset();
  wire(showPayload);
  vi.resetModules();
  await import("../src/main");
  await settle();
}

function clickFirstRow(): void {
  const row = document.querySelector<HTMLButtonElement>(".list-row");
  expect(row, "목록에 줄이 없다").toBeTruthy();
  row!.click();
}

describe("결정을 눌러 본문 열기", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  it("누르면 본문이 뜬다 — 화면이 비면 안 된다", async () => {
    await boot(FULL);
    clickFirstRow();
    await settle();

    const app = document.querySelector("#app")!;
    expect(app.textContent?.trim(), "화면이 통째로 비었다").not.toBe("");
    expect(document.querySelector(".reader"), "본문 자리가 없다").toBeTruthy();
    expect(document.querySelector(".reader-title")?.textContent).toContain("임베디드");
    expect(document.querySelector(".md")?.textContent).toContain("임베디드 DB 로 간다");
  });

  it("속성 패널이 뜬다", async () => {
    await boot(FULL);
    clickFirstRow();
    await settle();
    const keys = [...document.querySelectorAll(".props-key")].map((e) => e.textContent?.trim());
    for (const want of ["date", "status", "outcome", "tags", "supersedes"]) {
      expect(keys.some((k) => k?.includes(want)), `속성 ${want} 가 없다`).toBe(true);
    }
  });

  // ★ ① 판 차이 — 앱에 번들된 prior 가 낡으면 새 키가 안 온다.
  //
  // **이건 현장에서 반드시 일어난다.** 앱은 번들된 prior 를 폴백으로 쓰고 PATH 의
  // npm 판을 우선하는데, 둘의 판이 같을 이유가 없다.
  it("낡은 prior 가 키를 빼먹어도 안 죽는다", async () => {
    const old = { ...FULL } as Record<string, unknown>;
    delete old.summary_history;
    delete old.type;
    delete old.source_session;
    await boot(old);
    clickFirstRow();
    await settle();
    expect(document.querySelector("#app")!.textContent?.trim(), "화면이 비었다").not.toBe("");
    expect(document.querySelector(".reader-title")).toBeTruthy();
  });

  it("배열 속성이 통째로 없어도 안 죽는다", async () => {
    const broken = { ...FULL } as Record<string, unknown>;
    delete broken.tags;
    delete broken.supersedes;
    delete broken.related;
    delete broken.domain;
    await boot(broken);
    clickFirstRow();
    await settle();
    expect(document.querySelector(".reader-title")).toBeTruthy();
  });

  it("본문이 없어도 제목은 뜬다", async () => {
    const noBody = { ...FULL } as Record<string, unknown>;
    delete noBody.body;
    await boot(noBody);
    clickFirstRow();
    await settle();
    expect(document.querySelector(".reader-title")).toBeTruthy();
  });

  // ★ ③ 오류 경계 — 렌더가 어디서 터지든 **검은 화면이 되면 안 된다.**
  it("show 가 실패하면 오류가 보인다", async () => {
    document.body.innerHTML = '<div id="app"></div>';
    invoke.mockReset();
    invoke.mockImplementation((cmd: string) => {
      if (cmd === "list_notes") return Promise.resolve(JSON.stringify([ROW]));
      if (cmd === "show_note") return Promise.reject({ kind: "not_found", message: "prior 가 없다" });
      return Promise.resolve(JSON.stringify({ confirm: [], review: [], retro: [], health: [] }));
    });
    vi.resetModules();
    await import("../src/main");
    await settle();
    clickFirstRow();
    await settle();
    const txt = document.querySelector("#app")!.textContent ?? "";
    expect(txt.trim(), "화면이 비었다").not.toBe("");
    expect(txt).toContain("prior");
  });

  it("위키링크를 누르면 그 결정을 연다", async () => {
    await boot(FULL);
    clickFirstRow();
    await settle();
    invoke.mockClear();
    document.querySelector<HTMLButtonElement>("button.md-wiki-link")!.click();
    await settle();
    const call = invoke.mock.calls.find((c) => c[0] === "show_note");
    expect(call?.[1]).toEqual({ stem: "alpha-결정-스키마-2026-08-02" });
  });

  it("브레드크럼으로 목록에 돌아간다", async () => {
    await boot(FULL);
    clickFirstRow();
    await settle();
    document.querySelector<HTMLButtonElement>(".crumb-link")!.click();
    await settle();
    expect(document.querySelector(".list-row"), "목록으로 안 돌아왔다").toBeTruthy();
  });
});

/** ★ 오류 경계 — 렌더가 **어디서** 터지든 검은 화면이 되면 안 된다.
 *
 * 개별 방어(properties.ts 의 arr·str)와 별개로 필요하다. 다음에 어디서 터질지는
 * 모르지만, 터졌을 때 무엇이 터졌는지는 보여야 한다. */
describe("오류 경계", () => {
  it("본문이 문자열이 아니어도 화면이 안 비고 원인이 보인다", async () => {
    const weird = { ...FULL } as Record<string, unknown>;
    weird.body = { 이상한: "모양" };
    await boot(weird);
    clickFirstRow();
    await settle();
    const txt = document.querySelector("#app")!.textContent ?? "";
    expect(txt.trim(), "화면이 비었다").not.toBe("");
  });
});

/** ★★★ **눌러서 고치는 경로를 실제로 누른다.**
 *
 * 모드 버튼이 없어졌으므로(2026-09-01) 블록을 누르면 그 자리가 원문이 되어야 하고,
 * 포커스를 잃으면 **그 줄 범위에만** 갈아 끼워 저장해야 한다. 범위가 틀리면 고친
 * 글이 엉뚱한 줄을 덮어써서 결정문이 조용히 망가진다. */
describe("블록을 눌러 고치기", () => {
  const openFirst = async (): Promise<void> => {
    await boot(FULL);
    clickFirstRow();
    await settle();
  };
  const editorOf = () => document.querySelector<HTMLTextAreaElement>("textarea.md-editor");

  it("모드 버튼이 없다", async () => {
    await openFirst();
    const labels = [...document.querySelectorAll("button")].map((b) => b.textContent);
    expect(labels).not.toContain("본문 고치기");
  });

  it("문단을 누르면 그 자리가 원문이 된다", async () => {
    await openFirst();
    const p = document.querySelector<HTMLElement>("p.md-block")!;
    expect(p.textContent).toContain("임베디드 DB 로 간다");
    p.click();
    await settle();
    const ta = editorOf();
    expect(ta, "원문 상자가 안 열렸다").toBeTruthy();
    expect(ta!.value).toBe("임베디드 DB 로 간다.");
    // 나머지는 렌더된 채로 남아야 한다 — 문서 전체가 상자가 되면 안 된다.
    expect(document.querySelectorAll("textarea").length).toBe(1);
    expect(document.querySelector(".md-h"), "다른 블록이 사라졌다").toBeTruthy();
  });

  it("고치고 포커스를 잃으면 그 줄만 갈아 끼워 저장한다", async () => {
    await openFirst();
    document.querySelector<HTMLElement>("p.md-block")!.click();
    await settle();
    const ta = editorOf()!;
    ta.value = "SQLite 로 간다.";
    ta.dispatchEvent(new Event("input"));
    ta.dispatchEvent(new Event("blur"));
    await settle();

    const call = invoke.mock.calls.find((c) => c[0] === "save_body");
    expect(call, "저장이 안 나갔다").toBeTruthy();
    const sent = (call![1] as { body: string }).body;
    expect(sent).toContain("SQLite 로 간다.");
    // ★ 나머지 원문이 한 글자도 안 움직여야 한다.
    expect(sent).toContain("## 결정");
    expect(sent).toContain("- 하나\n  - 안쪽");
    expect(sent).toContain("[[alpha-결정-스키마-2026-08-02]]");
    expect(sent).not.toContain("임베디드 DB 로 간다");
  });

  it("안 바뀌었으면 저장하지 않는다 — 뜻 없는 커밋이 볼트에 쌓인다", async () => {
    await openFirst();
    document.querySelector<HTMLElement>("p.md-block")!.click();
    await settle();
    editorOf()!.dispatchEvent(new Event("blur"));
    await settle();
    expect(invoke.mock.calls.some((c) => c[0] === "save_body")).toBe(false);
  });

  it("Esc 는 되돌린다", async () => {
    await openFirst();
    document.querySelector<HTMLElement>("p.md-block")!.click();
    await settle();
    const ta = editorOf()!;
    ta.value = "버릴 글";
    ta.dispatchEvent(new Event("input"));
    ta.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
    await settle();
    expect(editorOf(), "상자가 안 닫혔다").toBeNull();
    expect(invoke.mock.calls.some((c) => c[0] === "save_body")).toBe(false);
  });

  it("위키링크를 눌러도 편집으로 안 빠진다 — 그건 이동이다", async () => {
    await openFirst();
    invoke.mockClear();
    document.querySelector<HTMLButtonElement>("button.md-wiki-link")!.click();
    await settle();
    expect(editorOf(), "링크를 눌렀는데 편집이 열렸다").toBeNull();
    expect(invoke.mock.calls.some((c) => c[0] === "show_note")).toBe(true);
  });

  it("목록 블록도 원문 그대로 열린다 — 중첩까지", async () => {
    await openFirst();
    document.querySelector<HTMLElement>("ul.md-block")!.click();
    await settle();
    expect(editorOf()!.value).toBe("- 하나\n  - 안쪽");
  });

  it("문서 끝에서 새 블록을 더한다", async () => {
    await openFirst();
    document.querySelector<HTMLElement>(".md-append")!.click();
    await settle();
    const ta = editorOf()!;
    expect(ta.value).toBe("");
    ta.value = "새로 쓴 줄";
    ta.dispatchEvent(new Event("input"));
    ta.dispatchEvent(new Event("blur"));
    await settle();
    const call = invoke.mock.calls.find((c) => c[0] === "save_body")!;
    const sent = (call[1] as { body: string }).body;
    expect(sent.endsWith("새로 쓴 줄") || sent.includes("새로 쓴 줄")).toBe(true);
    expect(sent).toContain("## 결정");
  });
});

/** ★★★ **속성 편집이 실제로 저장까지 간다.**
 *
 * 화면 단위 시험(properties.test.ts)은 콜백이 불리는 것까지만 본다. 조립부가
 * 그 콜백을 엉뚱한 명령에 연결하면 거기서는 안 잡힌다 — 이 저장소가 다섯 번
 * 겪은 그 사고다(wiring.test.ts 의 §). */
describe("속성을 고쳐 저장하기", () => {
  const openFirst = async (): Promise<void> => {
    await boot(FULL);
    clickFirstRow();
    await settle();
  };
  const cellOf = (key: string): HTMLElement =>
    [...document.querySelectorAll<HTMLElement>(".props-key")]
      .find((k) => k.textContent?.includes(key))!.nextElementSibling as HTMLElement;

  it("outcome 을 고르면 review_note 로 나간다", async () => {
    await openFirst();
    invoke.mockClear();
    const sel = cellOf("outcome").querySelector<HTMLSelectElement>("select")!;
    sel.value = "good";
    sel.dispatchEvent(new Event("change"));
    await settle();
    const call = invoke.mock.calls.find((c) => c[0] === "review_note");
    expect(call, "review_note 가 안 나갔다").toBeTruthy();
    expect(call![1]).toEqual({ stem: FULL.stem, outcome: "good" });
  });

  it("요약을 고치면 그 값만 나간다", async () => {
    await openFirst();
    invoke.mockClear();
    cellOf("summary").click();
    const ta = cellOf("summary").querySelector<HTMLTextAreaElement>("textarea")!;
    ta.value = "SQLite 로 고른다";
    ta.dispatchEvent(new Event("blur"));
    await settle();
    const call = invoke.mock.calls.find((c) => c[0] === "review_note")!;
    expect(call[1]).toEqual({ stem: FULL.stem, summary: "SQLite 로 고른다" });
  });

  it("태그를 갈면 목록으로 나간다", async () => {
    await openFirst();
    invoke.mockClear();
    cellOf("tags").click();
    const ta = cellOf("tags").querySelector<HTMLTextAreaElement>("textarea")!;
    ta.value = "저장엔진, 새태그";
    ta.dispatchEvent(new Event("blur"));
    await settle();
    const call = invoke.mock.calls.find((c) => c[0] === "review_note")!;
    expect(call[1]).toEqual({ stem: FULL.stem, tags: ["저장엔진", "새태그"] });
  });

  // ★ 고친 뒤 **다시 읽어야** 한다 — review 는 화면이 모르는 일을 한다
  // (옛 요약을 summary_history 로 옮기고 decision 표식을 지킨다).
  it("고친 뒤 노트를 다시 읽는다", async () => {
    await openFirst();
    invoke.mockClear();
    const sel = cellOf("outcome").querySelector<HTMLSelectElement>("select")!;
    sel.value = "bad";
    sel.dispatchEvent(new Event("change"));
    await settle();
    expect(invoke.mock.calls.some((c) => c[0] === "show_note"), "다시 안 읽었다").toBe(true);
  });

  it("못 고치는 칸은 눌러도 아무 명령이 안 나간다", async () => {
    await openFirst();
    invoke.mockClear();
    for (const locked of ["date", "author", "type"]) {
      cellOf(locked).click();
    }
    await settle();
    expect(invoke.mock.calls.some((c) => c[0] === "review_note")).toBe(false);
  });
});
