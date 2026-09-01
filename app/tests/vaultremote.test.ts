import { describe, it, expect } from "vitest";
import { renderVaults, type VaultActions } from "../src/render/vaults";
import type { Settings, VaultInfo } from "../src/types";

const vault = (o: Partial<VaultInfo>): VaultInfo => ({
  name: "default",
  path: "/v",
  exists: true,
  decisions: 0,
  domains: [],
  remote: "",
  ...o,
});

function screen(vaults: VaultInfo[]): { root: HTMLElement; calls: [string, string][] } {
  const calls: [string, string][] = [];
  const on: VaultActions = {
    open: () => {},
    add: () => {},
    bind: () => {},
    remote: (name, url) => calls.push([name, url]),
  };
  const s = { vault_parent: "/p", vaults, domains: [], hosts: [] } as unknown as Settings;
  const root = document.createElement("div");
  renderVaults(root, s, on);
  return { root, calls };
}

const inputs = (r: HTMLElement) =>
  [...r.querySelectorAll<HTMLInputElement>("input.vault-remote-input")];
const saveOf = (r: HTMLElement, i: number) =>
  [...r.querySelectorAll<HTMLButtonElement>(".vault-remote button")][i];

describe("볼트 리모트", () => {
  it("볼트마다 하나씩 있다 — 어느 볼트 얘기인지가 사라지면 회사 결정이 개인 리모트로 간다", () => {
    const { root } = screen([
      vault({ name: "default", remote: "https://github.com/me/personal.git" }),
      vault({ name: "work", remote: "" }),
    ]);
    const got = inputs(root);
    expect(got).toHaveLength(2);
    expect(got[0].value).toBe("https://github.com/me/personal.git");
    expect(got[1].value).toBe("");
  });

  it("리모트가 없는 것은 고장이 아니다 — 안내만 하고 경고로 그리지 않는다", () => {
    const { root } = screen([vault({ remote: "" })]);
    const box = root.querySelector(".vault-remote")!;
    expect(box.className).not.toContain("broken");
    expect(inputs(root)[0].placeholder).toContain("이 머신에만");
  });

  it("저장은 버튼을 눌러야 일어난다 — 치는 도중의 반쪽 주소가 origin 에 박히면 안 된다", () => {
    const { root, calls } = screen([vault({ name: "work", remote: "" })]);
    const input = inputs(root)[0];
    input.value = "https://git-codecommit.ap-northeast-2.amazonaws.com/v1/repos/vault";
    input.dispatchEvent(new Event("input"));
    expect(calls).toHaveLength(0); // 아직 저장 안 됨

    saveOf(root, 0).click();
    expect(calls).toEqual([
      ["work", "https://git-codecommit.ap-northeast-2.amazonaws.com/v1/repos/vault"],
    ]);
  });

  it("안 바뀌었거나 비었으면 저장 버튼이 죽어 있다", () => {
    const { root } = screen([vault({ name: "work", remote: "https://a.example/x.git" })]);
    expect(saveOf(root, 0).disabled).toBe(true); // 그대로다

    const input = inputs(root)[0];
    input.value = "   ";
    input.dispatchEvent(new Event("input"));
    expect(saveOf(root, 0).disabled).toBe(true); // 빈 값이다

    input.value = "https://b.example/y.git";
    input.dispatchEvent(new Event("input"));
    expect(saveOf(root, 0).disabled).toBe(false);
  });

  it("자리가 없는 볼트에는 못 붙인다 — 눌러도 아무 일이 안 나는 버튼을 두지 않는다", () => {
    const { root } = screen([vault({ name: "gone", exists: false })]);
    expect(inputs(root)[0].disabled).toBe(true);
    expect(saveOf(root, 0).disabled).toBe(true);
  });

  it("앞뒤 공백은 잘라서 넘긴다", () => {
    const { root, calls } = screen([vault({ name: "work", remote: "" })]);
    const input = inputs(root)[0];
    input.value = "  https://a.example/x.git  ";
    input.dispatchEvent(new Event("input"));
    saveOf(root, 0).click();
    expect(calls[0][1]).toBe("https://a.example/x.git");
  });
});

// ── 라벨을 없앤 뒤의 계약 ────────────────────────────────────────────
//
// 2026-09-01 사용자 지적: 이 화면이 실제 화면에서 **깨져 보였다.** 라벨 열을
// 58px 로 고정했는데 `git 리모트` 가 그보다 넓어 입력창 밑으로 깔렸고, 그 바람에
// 입력창이 안 늘어나 주소가 `https://github.com/xian0310` 에서 잘렸다.
//
// 고정 폭을 넓히는 대신 **라벨을 없앴다.** 한국어 라벨은 길이가 제각각이라
// 어떤 고정 폭을 골라도 다음 라벨에서 또 깨진다. 대신 placeholder 가 설명한다 —
// 경로는 보면 알고, 리모트 칸은 비어 있을 때 무엇을 넣는 자리인지 말해야 한다.
//
// 그 대가로 **눈에 보이는 이름이 사라진다.** 스크린리더는 placeholder 를 이름으로
// 쓰지 않으므로 aria-label 이 반드시 있어야 한다. 아래 시험이 그것을 잠근다.

describe("라벨 없는 리모트 칸", () => {
  it("★ 접근성 이름이 있다 — 눈에 보이는 라벨을 없앴으므로", () => {
    const { root } = screen([vault({ name: "회사" })]);
    const got = inputs(root)[0];
    const name = got.getAttribute("aria-label") ?? "";
    expect(name, "이름이 없다 — 스크린리더에서 무명 입력이 된다").not.toBe("");
    // **어느 볼트의 칸인지가 이름에 있어야 한다.** 볼트가 여럿이면 "리모트" 만으로는
    // 회사 결정을 개인 리모트에 밀어 넣는 사고를 막을 수 없다.
    expect(name).toContain("회사");
  });

  it("★ 빈 칸이 무엇을 넣는 자리인지 말한다", () => {
    const { root } = screen([vault({ remote: "" })]);
    const ph = inputs(root)[0].placeholder;
    expect(ph, "무엇을 넣는 칸인지 모른다").toMatch(/리모트|주소/);
    // 비어 있는 것이 고장이 아니라는 사실도 그대로 남는다.
    expect(ph).toContain("이 머신에만");
  });

  it("★ 긴 경로는 잘려도 전체를 볼 수 있다", () => {
    const long = "/Users/eonghan/Documents/아주 긴 경로/Obsidian Vault";
    const { root } = screen([vault({ path: long })]);
    const p = root.querySelector<HTMLElement>(".vault-path")!;
    // 한 줄로 줄여 그리므로, 전체 값은 title 로 남아야 한다.
    expect(p.title, "잘린 경로의 전체를 볼 길이 없다").toBe(long);
  });

  it("★ 고정 폭 라벨 열은 없앤다 — 다음 라벨에서 또 깨진다", () => {
    const { root } = screen([vault({})]);
    expect(root.querySelector(".vault-field-label"), "고정 폭 라벨이 남아 있다").toBeNull();
  });
});
