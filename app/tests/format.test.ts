import { describe, it, expect } from "vitest";
import { vaultLabel, clip, reasonLabel } from "../src/format";

describe("vaultLabel", () => {
  it("도메인과 볼트를 같이 보여 준다", () => {
    expect(vaultLabel("priorcase", "personal")).toBe("priorcase · personal 볼트");
  });

  // ★★ 볼트가 비면 설정 오류다. 기본 볼트로 그리면 사람은 거기 쌓이는 줄 알지만
  //    실제로는 기록 자체가 실패한다 (capture 가 없는 볼트를 거부한다).
  it("볼트를 모르면 그렇다고 말한다", () => {
    expect(vaultLabel("priorcase", "")).toBe("priorcase · ⚠️ 볼트 미상");
  });
});

describe("clip", () => {
  it("줄 수를 넘으면 접고 몇 줄이 남았는지 준다", () => {
    const text = ["a", "b", "c", "d", "e"].join("\n");
    expect(clip(text, 3)).toEqual({ shown: "a\nb\nc", hidden: 2 });
  });

  it("짧으면 그대로 준다", () => {
    expect(clip("a\nb", 3)).toEqual({ shown: "a\nb", hidden: 0 });
  });

  it("딱 맞으면 접지 않는다", () => {
    expect(clip("a\nb\nc", 3)).toEqual({ shown: "a\nb\nc", hidden: 0 });
  });

  // ★ 발췌가 실측 880B~4.9KB 이고 절반이 한 화면에 안 들어간다.
  //   접는 것 자체보다 "몇 줄이 숨었나" 를 아는 것이 중요하다.
  it("빈 문자열도 터지지 않는다", () => {
    expect(clip("", 3)).toEqual({ shown: "", hidden: 0 });
  });

  // ★★ **숨은 줄 수는 실제로 안 보이는 줄 수여야 한다.**
  //
  // shown 을 잘라 놓고 hidden 을 상수나 어림수로 내면, 사람은 "3줄 더" 를 믿고
  // 펼쳤다가 40줄을 만난다. 계산이 아니라 **약속**이므로 대조해서 지킨다.
  it("접힌 줄 수가 실제로 사라진 줄 수와 같다", () => {
    const text = Array.from({ length: 40 }, (_, i) => `줄${i}`).join("\n");
    const r = clip(text, 6);
    expect(r.shown.split("\n").length).toBe(6);
    expect(r.shown.split("\n").length + r.hidden).toBe(40);
  });
});

describe("reasonLabel", () => {
  it("재회수는 횟수를 보여 준다", () => {
    expect(reasonLabel("recalled", 4)).toBe("재회수 4회");
  });

  // ★ superseded 는 hits 가 0 일 수 있다 — "재회수 0회" 로 그리면 거짓이다.
  it("뒤집힌 것은 횟수를 말하지 않는다", () => {
    expect(reasonLabel("superseded", 0)).toBe("뒤집혔다");
    expect(reasonLabel("superseded", 3)).toBe("뒤집혔다");
  });

  // ★★ **모르는 방아쇠를 재회수로 뭉개면 안 된다.**
  //
  // 지금 Go 쪽 Reason 은 둘뿐이지만 늘어날 수 있고, TS 타입은 런타임 JSON 을
  // 검사하지 않는다. else 로 "재회수 N회" 를 내면 **새 방아쇠가 전부 거짓말로
  // 그려지고 아무도 눈치채지 못한다.** 모르면 모른다고 보이게 둔다.
  it("모르는 방아쇠는 재회수로 뭉개지 않는다", () => {
    expect(reasonLabel("archived", 0)).toBe("archived");
    expect(reasonLabel("archived", 7)).toBe("archived");
  });
});
