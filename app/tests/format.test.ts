import { describe, it, expect } from "vitest";
import { num, hostState, vaultState, vaultOfDomain, backlogLine } from "../src/format";
import type { HostInfo, VaultInfo } from "../src/types";

const host = (o: Partial<HostInfo>): HostInfo => ({
  name: "Codex CLI",
  enabled: true,
  root: "/h",
  exists: true,
  files: 0,
  ...o,
});

const vault = (o: Partial<VaultInfo>): VaultInfo => ({
  name: "default",
  path: "/v",
  exists: true,
  decisions: 0,
  domains: [],
  ...o,
});

describe("num", () => {
  // ★ 기록 1729개는 한눈에 안 읽힌다. 자릿점이 있어야 "천 단위구나" 가 보인다.
  it("자릿점을 찍는다", () => {
    expect(num(1729)).toBe("1,729");
    expect(num(0)).toBe("0");
  });
});

describe("hostState", () => {
  // ★★★ **"자리 없음" 과 "기록 0개" 는 다른 말이다.**
  //
  // 앞엣것은 그 도구를 안 쓰거나 기록 자리를 옮긴 것이고, 뒤엣것은 도구는
  // 있는데 대화가 없는 것이다. 뭉치면 사람이 무엇을 고쳐야 할지 모른다.
  it("자리가 없는 것과 비어 있는 것을 가른다", () => {
    expect(hostState(host({ exists: false, files: 0 }))).toContain("자리가 없다");
    expect(hostState(host({ exists: true, files: 0 }))).toBe("대화 0개");
    expect(hostState(host({ exists: true, files: 1729 }))).toBe("대화 1,729개");
  });
});

describe("vaultState", () => {
  // ★★★ 자리가 없으면 **그것부터 말한다.** 결정 0건으로 그리면 "아직 안
  //     썼구나" 로 읽히는데, 실제로는 그 볼트로 엮인 도메인의 기록이 통째로
  //     안 써지는 상태다.
  it("자리가 없으면 개수보다 그것을 먼저 말한다", () => {
    const s = vaultState(vault({ exists: false, decisions: 0 }));
    expect(s).toContain("자리가 없다");
    expect(s).not.toContain("0건");
  });

  it("정상이면 결정과 프로젝트 수를 낸다", () => {
    expect(vaultState(vault({ decisions: 156, domains: ["a", "b"] }))).toBe(
      "결정 156건 · 프로젝트 2개",
    );
  });
});

describe("vaultOfDomain", () => {
  // ★★★ **빈 값은 "기본 볼트" 라는 뜻이다.**
  //
  // 빈칸으로 그리면 사람은 "엮이지 않았다" 고 읽는데, 실제로는 기본 볼트로
  // 잘 가고 있다. 그 오해가 쓸데없는 재설정을 부른다.
  it("빈 값은 기본 볼트로 읽는다", () => {
    expect(vaultOfDomain("", "default")).toBe("default");
    expect(vaultOfDomain("work", "default")).toBe("work");
  });
});

describe("backlogLine", () => {
  // ★★ 밀린 것이 없으면 **한 글자도 안 낸다.** 늘 무언가 떠 있으면 사람이
  //    그것을 무시하는 법을 배운다.
  it("밀린 것이 없으면 빈 문자열", () => {
    expect(backlogLine(0, 0)).toBe("");
  });

  it("종류마다 다른 말을 쓴다", () => {
    expect(backlogLine(28, 0)).toContain("판정을 기다리는");
    expect(backlogLine(0, 55)).toContain("결과를 안 물어본");
  });

  it("둘 다 있으면 이어 붙인다", () => {
    const s = backlogLine(28, 55);
    expect(s).toContain("28건");
    expect(s).toContain("55건");
  });
});
