import { describe, it, expect, vi } from "vitest";

// main.ts 는 최상위에서 IPC 를 부를 수 있으므로 가짜로 막는다.
// (지금 구현은 #app 이 없으면 아무것도 시작하지 않지만, 그 사실에 시험이
//  기대지 않게 둔다 — 부트스트랩이 바뀌어도 이 파일은 안 깨져야 한다.)
vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn().mockResolvedValue("{}") }));

const { badgeText } = await import("../src/main");
import type { Queue } from "../src/types";

const empty: Queue = { confirm: [], review: [], retro: [], health: [] };
const n = (k: number) => Array.from({ length: k }, () => ({}) as never);

describe("badgeText", () => {
  // ★★ 할 일이 없으면 숫자를 안 붙인다. 늘 무언가 떠 있으면 사람이 그것을
  //    무시하는 법을 배우고, 그러면 진짜 할 일이 있을 때도 안 보인다.
  it("할 일이 없으면 빈 문자열", () => {
    expect(badgeText(empty)).toBe("");
  });

  // ★★ **합계 하나로 낸다.** 메뉴바는 폭이 좁다 — 종류별로 나열하면
  //    "확인 23 · 검토 3 · 회고 50" 처럼 길어져 다른 아이콘을 밀어낸다.
  it("세 큐를 합쳐 하나로 낸다", () => {
    expect(badgeText({ ...empty, confirm: n(23), review: n(3), retro: n(50) })).toBe("76");
  });

  it("한 종류만 있어도 합계다", () => {
    expect(badgeText({ ...empty, review: n(1) })).toBe("1");
    expect(badgeText({ ...empty, retro: n(50) })).toBe("50");
  });

  // ★★★ 상태에 fail 이 있으면 큐가 비어도 알린다 — **고장이 조용하면 안 된다.**
  //     이 시스템은 모든 부품이 실패해도 대화를 막지 않도록 설계돼 있어서,
  //     앱마저 조용하면 고장을 알아챌 자리가 아무 데도 없다.
  it("고장은 큐가 비어도 알린다", () => {
    expect(badgeText({ ...empty, health: [{ name: "볼트", level: "fail", detail: "x" }] })).toBe(
      "⚠",
    );
  });

  // ★★ 고장과 할 일이 같이 있으면 **둘 다** 보여야 한다. 고장만 보이면 큐가
  //    빈 줄 알고, 숫자만 보이면 고장을 놓친다.
  it("고장이면서 할 일도 있으면 둘 다 낸다", () => {
    const q: Queue = {
      ...empty,
      confirm: n(23),
      review: n(3),
      retro: n(50),
      health: [{ name: "볼트", level: "fail", detail: "x" }],
    };
    expect(badgeText(q)).toBe("⚠76");
  });

  // ★ warn 은 고장이 아니다. warn 으로 ⚠ 를 띄우면 실측 상태 10건 중 2건이
  //   상시 warn 이라(팀 이식성·색인) 배지가 영구히 켜진다.
  it("warn 으로는 경고 표시를 안 띄운다", () => {
    const q: Queue = {
      ...empty,
      confirm: n(2),
      health: [
        { name: "팀 이식성", level: "warn", detail: "x" },
        { name: "색인", level: "warn", detail: "y" },
      ],
    };
    expect(badgeText(q)).toBe("2");
  });

  // ★ 모르는 등급을 fail 로 읽지 않는다 — 그러면 등급이 하나 늘 때마다 배지가
  //   경고로 굳는다. (안 보이게 뭉개는 것과는 다른 문제다. 상태 화면은
  //   모르는 등급을 unknown 으로 드러낸다.)
  it("모르는 등급으로는 경고 표시를 안 띄운다", () => {
    const q: Queue = {
      ...empty,
      health: [{ name: "새검사", level: "unknown", detail: "x" }],
    };
    expect(badgeText(q)).toBe("");
  });
});
