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
