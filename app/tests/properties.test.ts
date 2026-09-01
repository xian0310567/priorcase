import { describe, it, expect } from "vitest";
import { renderProperties, editableKeys } from "../src/render/properties";
import type { NoteFull } from "../src/types";

/** ★★★ **속성도 고칠 수 있어야 한다** (2026-09-01 사업주 요구).
 *
 * 다만 아무거나는 아니다. 통로가 `prior review` 라 그것이 아는 필드만 열린다.
 *
 * # 왜 어떤 것은 안 열리는가
 *
 *   date·domain  파일명이 그 둘을 담는다({domain}-결정-{slug}-{date}) — 고치면
 *                파일이 옮겨져야 하고, 그건 위키링크를 전부 끊는다.
 *   type         이 노트가 결정이라는 사실 자체다. 바꾸면 회수 대상에서 빠진다.
 *   author·source_session·vault  기록된 사실이지 판단이 아니다.
 *   supersedes   `--reason` 이 같이 필요하다 — 한 칸짜리 편집으로 낼 수 없다.
 *
 * 열리는 것만 열고 나머지는 **왜 안 되는지 보이게** 둔다. 못 고치는 칸이 고칠 수
 * 있는 것처럼 보이는 쪽이 더 나쁘다. */
const NOTE: NoteFull = {
  stem: "alpha-결정-저장엔진-2026-08-01", path: "/v/a.md", rel: "a.md", vault: "default",
  domain: ["alpha"], date: "2026-08-01", status: "active", outcome: "pending",
  summary: "저장 엔진을 임베디드 DB 로 고른다", tags: ["decision", "저장엔진"],
  body: "본문", supersedes: [], related: [], author: "LeeJeongHan",
  superseded_reason: "", type: "decision", source_session: "abc", summary_history: [],
};

function screen(n: NoteFull = NOTE) {
  const root = document.createElement("div");
  const calls: unknown[] = [];
  renderProperties(root, n, {
    open: () => {},
    review: (patch) => calls.push(patch),
  });
  return { root, calls };
}

const rowOf = (root: HTMLElement, key: string): HTMLElement =>
  [...root.querySelectorAll<HTMLElement>(".props-key")]
    .find((k) => k.textContent?.includes(key))!.nextElementSibling as HTMLElement;

describe("속성 편집", () => {
  it("고칠 수 있는 것과 없는 것이 갈린다", () => {
    expect(editableKeys).toEqual(["summary", "status", "outcome", "tags"]);
    for (const locked of ["type", "date", "author", "domain", "vault", "source_session", "supersedes"]) {
      expect(editableKeys, `${locked} 는 열리면 안 된다`).not.toContain(locked);
    }
  });

  it("status·outcome 은 고르는 칸이다 — 허용값 밖을 못 넣는다", () => {
    const { root } = screen();
    const sel = rowOf(root, "status").querySelector<HTMLSelectElement>("select")!;
    expect([...sel.options].map((o) => o.value)).toEqual(["active", "superseded", "regretted", "retracted"]);
    expect(sel.value).toBe("active");
    const out = rowOf(root, "outcome").querySelector<HTMLSelectElement>("select")!;
    expect([...out.options].map((o) => o.value)).toEqual(["pending", "good", "bad"]);
  });

  it("outcome 을 고르면 그 값만 보낸다", () => {
    const { root, calls } = screen();
    const sel = rowOf(root, "outcome").querySelector<HTMLSelectElement>("select")!;
    sel.value = "good";
    sel.dispatchEvent(new Event("change"));
    expect(calls).toEqual([{ outcome: "good" }]);
  });

  // ★ 철회는 이유가 없으면 CLI 가 거부한다 — 화면에서 먼저 막는다.
  it("철회는 회고 없이 못 보낸다", () => {
    const { root, calls } = screen();
    const sel = rowOf(root, "status").querySelector<HTMLSelectElement>("select")!;
    sel.value = "retracted";
    sel.dispatchEvent(new Event("change"));
    expect(calls, "이유 없이 철회가 나갔다").toEqual([]);
    expect(root.textContent).toContain("이유");
  });

  it("요약은 눌러서 고치고 포커스를 잃을 때 보낸다", () => {
    const { root, calls } = screen();
    rowOf(root, "summary").click();
    const ta = rowOf(root, "summary").querySelector<HTMLTextAreaElement>("textarea")!;
    expect(ta.value).toBe(NOTE.summary);
    ta.value = "SQLite 로 고른다";
    ta.dispatchEvent(new Event("blur"));
    expect(calls).toEqual([{ summary: "SQLite 로 고른다" }]);
  });

  it("안 바뀐 요약은 안 보낸다", () => {
    const { root, calls } = screen();
    rowOf(root, "summary").click();
    rowOf(root, "summary").querySelector<HTMLTextAreaElement>("textarea")!.dispatchEvent(new Event("blur"));
    expect(calls).toEqual([]);
  });

  it("태그를 통째로 갈아 보낸다 — decision 표식은 안 보낸다", () => {
    const { root, calls } = screen();
    rowOf(root, "tags").click();
    const ta = rowOf(root, "tags").querySelector<HTMLTextAreaElement>("textarea")!;
    expect(ta.value).toBe("저장엔진");
    ta.value = "저장엔진, 영속성, 새것";
    ta.dispatchEvent(new Event("blur"));
    expect(calls).toEqual([{ tags: ["저장엔진", "영속성", "새것"] }]);
  });

  it("태그를 비우면 빈 목록을 보낸다 — 변경 없음과 달라야 한다", () => {
    const { root, calls } = screen();
    rowOf(root, "tags").click();
    const ta = rowOf(root, "tags").querySelector<HTMLTextAreaElement>("textarea")!;
    ta.value = "  ";
    ta.dispatchEvent(new Event("blur"));
    expect(calls).toEqual([{ tags: [] }]);
  });

  it("못 고치는 칸은 눌러도 상자가 안 열린다", () => {
    const { root } = screen();
    for (const locked of ["date", "author", "type", "source_session"]) {
      rowOf(root, locked).click();
      expect(rowOf(root, locked).querySelector("textarea"), `${locked} 가 열렸다`).toBeNull();
    }
  });
});
