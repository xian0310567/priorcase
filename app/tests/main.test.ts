import { describe, it, expect, vi } from "vitest";

// main.ts 는 최상위에서 IPC 를 부를 수 있으므로 가짜로 막는다.
// (지금 구현은 #app 이 없으면 아무것도 시작하지 않지만, 그 사실에 시험이
//  기대지 않게 둔다 — 부트스트랩이 바뀌어도 이 파일은 안 깨져야 한다.)
vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn().mockResolvedValue("{}") }));

const { badgeText } = await import("../src/main");
import type { Queue } from "../src/types";

const empty: Queue = { confirm: [], review: [], retro: [], health: [] };
const n = (k: number) => Array.from({ length: k }, () => ({}) as never);

// ★★★ **메뉴바는 고장났을 때만 말한다.**
//
// 2026-08-14 에 바뀐 규칙이다. 예전에는 큐 건수를 띄웠는데, 그건 "사람이 할
// 일이 있다" 는 뜻이었다. 확인·검토·회고 큐를 들어내면서 앱에는 사람이 눌러야
// 할 일감이 없어졌다 — 밀린 구간은 데몬이 소화한다.
//
// 그런데도 숫자를 띄우면 그것은 **무시하는 법을 가르치는 신호**가 된다. 그러면
// 진짜 고장이 났을 때도 안 보인다.
describe("badgeText", () => {
  it("정상이면 빈 문자열", () => {
    expect(badgeText(empty)).toBe("");
  });

  // ★★★ 밀린 일감이 아무리 많아도 **사람을 부르지 않는다.** 그건 데몬의
  //     처리량 문제이지 사람이 누를 일이 아니다.
  it("밀린 구간이 많아도 조용하다", () => {
    expect(badgeText({ ...empty, confirm: n(28), review: n(3), retro: n(55) })).toBe("");
  });

  // ★★★ 고장은 알린다 — **고장이 조용하면 안 된다.**
  //     이 시스템은 모든 부품이 실패해도 대화를 막지 않도록 설계돼 있어서,
  //     앱마저 조용하면 고장을 알아챌 자리가 아무 데도 없다.
  it("fail 이 하나라도 있으면 알린다", () => {
    expect(badgeText({ ...empty, health: [{ name: "볼트", level: "fail", detail: "x" }] })).toBe(
      "⚠",
    );
  });

  // ★★ warn 은 고장이 아니다. warn 으로 ⚠ 를 띄우면 실측 상태 10건 중 2건이
  //    상시 warn 이라(팀 이식성·색인) 배지가 영구히 켜진다.
  it("warn 으로는 안 띄운다", () => {
    const q: Queue = {
      ...empty,
      health: [
        { name: "팀 이식성", level: "warn", detail: "x" },
        { name: "색인", level: "warn", detail: "y" },
      ],
    };
    expect(badgeText(q)).toBe("");
  });

  // ★ 모르는 등급을 fail 로 읽지 않는다 — 그러면 등급이 하나 늘 때마다 배지가
  //   경고로 굳는다. (안 보이게 뭉개는 것과는 다른 문제다. 상태 화면은
  //   모르는 등급을 unknown 으로 드러낸다.)
  it("모르는 등급으로는 안 띄운다", () => {
    const q: Queue = {
      ...empty,
      health: [{ name: "새검사", level: "unknown", detail: "x" }],
    };
    expect(badgeText(q)).toBe("");
  });
});
