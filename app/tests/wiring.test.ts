import { describe, it, expect, vi, beforeAll } from "vitest";
import type { Settings } from "../src/types";

// ★★★ **배선을 화면에서 눌러 확인한다.**
//
// 이 프로젝트에서만 "필드·함수는 있는데 조립부가 안 붙였다" 가 다섯 번 나왔다
// (similarFor · sweepOthers · 데몬 정리 · 확인 큐 볼트 · 검토 큐 볼트).
// 공통점이 분명하다 — **손으로 만든 객체로 하는 시험은 조립부를 안 지나간다.**
//
// 여기서는 진짜 #app 에 앱을 띄우고, 진짜 위젯을 조작해, **어떤 IPC 가 나가는지**를
// 본다. 화면 함수가 옳아도 main.ts 가 엉뚱한 명령에 연결하면 여기서 잡힌다.
// 설정 화면에서 그 사고는 특히 조용하다 — 껐는데 계속 훑히거나, 볼트를 엮었는데
// 기록이 옛 볼트로 계속 간다.

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

const SETTINGS: Settings = {
  config_path: "/home/x/.config/priorcase/config.toml",
  vault_parent: "/v",
  vaults: [
    { name: "default", path: "/v/기본 볼트", exists: true, decisions: 12, domains: ["proj"], remote: "" },
    { name: "work", path: "/v/work", exists: false, decisions: 0, domains: [], remote: "" },
  ],
  domains: [
    { prefix: "proj", folder: "proj", vault: "", paths: ["/p"], repos: [] },
    { prefix: "other", folder: "other", vault: "work", paths: [], repos: [] },
  ],
  hosts: [
    { name: "Claude Code", enabled: true, root: "/h/claude", exists: true, files: 656 },
    { name: "Codex CLI", enabled: false, root: "/h/codex", exists: true, files: 1729 },
  ],
};

const QUEUE = JSON.stringify({
  confirm: [{ id: "a" }, { id: "b" }],
  review: [],
  retro: [{ stem: "x" }],
  health: [{ name: "볼트", level: "ok", detail: "/a" }],
});

const NOTES = JSON.stringify([
  {
    stem: "proj-결정-저장엔진-2026-08-01", path: "/v/a.md", rel: "a.md", vault: "default",
    domain: ["proj"], date: "2026-08-01", status: "active", outcome: "pending",
    summary: "저장 엔진을 고른다", tags: ["decision"],
  },
]);

function wire(): void {
  invoke.mockImplementation((cmd: string) => {
    if (cmd === "queue") return Promise.resolve(QUEUE);
    if (cmd === "settings") return Promise.resolve(JSON.stringify(SETTINGS));
    if (cmd === "list_notes") return Promise.resolve(NOTES);
    if (cmd === "show_note") return Promise.resolve(JSON.stringify({
      ...JSON.parse(NOTES)[0], body: "## 본문\n\n내용이다.\n",
      supersedes: [], related: [], author: "", superseded_reason: "",
    }));
    if (cmd === "search_notes") return Promise.resolve("[]");
    return Promise.resolve(undefined);
  });
}

/** goSettings 는 사이드바의 톱니바퀴를 눌러 설정 화면으로 간다.
 *
 * **앱의 첫 화면은 볼트다**(2026-09-01 결정) — 설정은 곁다리로 밀렸다.
 * 설정 배선을 누르려면 먼저 그리로 가야 한다. */
async function goSettings(): Promise<void> {
  const gear = document.querySelector<HTMLButtonElement>(".side-settings");
  if (gear) {
    gear.click();
    await settle();
  }
}

/** 폴링이 한 바퀴 돌기를 기다린다. */
async function settle(): Promise<void> {
  for (let i = 0; i < 20; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

/** navItem 은 설정 사이드바의 칸 하나다.
 *
 * 2026-09-01 설정이 탭 줄에서 **사이드바**로 옮겼다 (탭에 `볼트` 가 두 번 나와서).
 * `.settings-shell` 로 한정하는 것이 중요하다 — 볼트 화면의 도메인 목록도 같은
 * `.side-item` 을 쓰므로, 한정하지 않으면 엉뚱한 것을 누른다. */
function navItem(label: string): HTMLButtonElement {
  return Array.from(
    document.querySelectorAll<HTMLButtonElement>(".settings-shell .side-item"),
  ).find((b) => b.textContent?.trim() === label)!;
}

/** 마지막으로 나간 쓰기 명령. 읽기·트레이는 뺀다. */
function lastWrite(): [string, unknown] | undefined {
  const w = invoke.mock.calls.filter(
    (c) => c[0] !== "queue" && c[0] !== "settings" && c[0] !== "set_tray_title",
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
  // ★ **첫 화면은 볼트다.** 설정이 아니다 (2026-09-01 기획 변경).
  it("볼트 화면으로 뜨고 설정은 톱니바퀴 뒤에 있다", () => {
    expect(document.querySelector(".browse"), "볼트 화면이 아니다").toBeTruthy();
    expect(document.querySelector(".settings-shell"), "설정이 바로 보인다").toBeFalsy();
    expect(document.querySelector(".side-settings"), "톱니바퀴가 없다").toBeTruthy();
  });

  it("톱니바퀴를 누르면 설정 칸 셋이 뜬다 — 큐 화면은 없다", async () => {
    await goSettings();
    const labels = Array.from(document.querySelectorAll(".settings-shell .side-item"))
      .map((b) => b.textContent?.trim());
    expect(labels).toEqual(["호스트", "볼트", "상태"]);
    // ★ 나가는 문은 **돌아갈 곳의 이름을 안 쓴다.** 예전에는 `‹ 볼트` 라서
    //   설정의 `볼트` 칸과 같은 낱말이 두 번 나왔다.
    expect(document.querySelector(".side-back")?.textContent).not.toContain("볼트");
  });

  // ★★★ **고장이 없으면 메뉴바에 글자가 없다.**
  //
  // 예전에는 큐 건수를 띄웠다. 지금은 사람이 누를 일감이 없으므로 숫자를 띄우면
  // 그것은 "무시하는 법" 을 가르치는 신호가 된다.
  it("트레이 글자는 고장이 없으면 빈 문자열이다", () => {
    const calls = invoke.mock.calls.filter((c) => c[0] === "set_tray_title");
    expect(calls.length, "트레이를 한 번도 안 갱신했다").toBeGreaterThan(0);
    expect(calls[calls.length - 1][1]).toEqual({ title: "" });
  });

  // ★★★ **호스트 토글이 set_host 로 나가야 한다.**
  //
  // 여기가 어긋나면 사람은 껐다고 믿는데 대화는 계속 훑힌다 — 조용한 실패다.
  it("호스트 체크박스가 set_host 를 이름 그대로 부른다", async () => {
    await goSettings();
    const boxes = document.querySelectorAll<HTMLInputElement>("input.host-toggle");
    expect(boxes.length).toBe(2);
    // 꺼져 있는 Codex 를 켠다.
    boxes[1].checked = true;
    boxes[1].dispatchEvent(new Event("change"));
    await settle();
    expect(lastWrite()).toEqual(["set_host", { name: "Codex CLI", enabled: true }]);
  });

  it("체크 상태가 설정을 그대로 비춘다", async () => {
    await goSettings();
    const boxes = document.querySelectorAll<HTMLInputElement>("input.host-toggle");
    expect(boxes[0].checked).toBe(true);
    expect(boxes[1].checked).toBe(false);
  });

  // ★★★ **기본 볼트를 고르면 빈 값으로 나가야 한다.**
  //
  // "default" 라는 이름을 그대로 보내면 설정에 vault 줄이 남고, 나중에 기본
  // 볼트의 이름이 바뀌면 그 프로젝트만 갈 곳을 잃는다.
  it("볼트 엮기가 bind_domain 을 부르고 기본은 빈 값으로 보낸다", async () => {
    await goSettings();
    navItem("볼트").click();
    await settle();
    const sels = document.querySelectorAll<HTMLSelectElement>("select.domain-vault");
    expect(sels.length).toBe(2);

    sels[0].value = "work";
    sels[0].dispatchEvent(new Event("change"));
    await settle();
    expect(lastWrite()).toEqual(["bind_domain", { prefix: "proj", vault: "work" }]);

    navItem("볼트").click();
    await settle();
    const again = document.querySelectorAll<HTMLSelectElement>("select.domain-vault");
    again[1].value = "default";
    again[1].dispatchEvent(new Event("change"));
    await settle();
    expect(lastWrite()).toEqual(["bind_domain", { prefix: "other", vault: "" }]);
  });

  it("[열기] 는 볼트 이름으로 open_vault 를 부른다", async () => {
    navItem("볼트").click();
    await settle();
    const btns = document.querySelectorAll<HTMLButtonElement>(".vault-card .vault-open");
    btns[0].click();
    await settle();
    expect(lastWrite()).toEqual(["open_vault", { name: "default" }]);
  });

  it("자리가 없는 볼트는 [열기] 가 막혀 있다", async () => {
    navItem("볼트").click();
    await settle();
    const btns = document.querySelectorAll<HTMLButtonElement>(".vault-card .vault-open");
    // ★ 카드마다 열기가 **하나씩** 이어야 한다. 예전에는 카드 안의 버튼을
    //   순서로 집어서 두 번째가 첫 볼트의 [저장] 이었고, 그것도 막혀 있어
    //   이 단언이 우연히 통과했다.
    expect(btns).toHaveLength(2);
    expect(btns[1].disabled, "자리가 없는데 열기가 눌린다").toBe(true);
  });

  // ★★★ **경로를 묻지 않는다. 대신 어디에 생기는지 보여 준다.**
  //
  // 경로를 안 물어봤으므로 그 줄이 없으면 사람은 어디에 만들어졌는지 모르는
  // 폴더를 갖게 된다 — 그건 묻는 것보다 나쁘다.
  it("볼트 만들기가 이름만으로 add_vault 를 부르고 자리를 미리 보여 준다", async () => {
    navItem("볼트").click();
    await settle();
    expect(
      document.querySelector(".vault-add-path"),
      "경로 입력이 아직 있다",
    ).toBeNull();

    const name = document.querySelector<HTMLInputElement>(".vault-add-name")!;
    const btn = document.querySelector<HTMLButtonElement>(".vault-add button")!;
    const where = document.querySelector<HTMLElement>(".vault-add-where")!;
    expect(btn.disabled, "빈 칸인데 만들기가 눌린다").toBe(true);
    expect(where.textContent, "빈 칸인데 자리를 말한다").toBe("");

    name.value = "회사";
    name.dispatchEvent(new Event("input"));
    expect(btn.disabled).toBe(false);
    expect(where.textContent, "어디에 생기는지 안 보여 준다").toBe("/v/회사");

    btn.click();
    await settle();
    expect(lastWrite()).toEqual(["add_vault", { name: "회사" }]);
  });

  // ★★★ **밀린 구간은 상태의 진단 한 줄이지 할 일 목록이 아니다.**
  it("상태 탭이 검사와 밀린 건수를 그린다", async () => {
    navItem("상태").click();
    await settle();
    expect(document.querySelectorAll(".health-row").length).toBe(1);
    const backlog = document.querySelector(".backlog")!;
    expect(backlog.textContent).toContain("2건");
    expect(backlog.textContent).toContain("1건");
    // 누를 것이 없어야 한다.
    expect(document.querySelectorAll(".backlog button").length).toBe(0);
  });
});
