import { describe, it, expect, vi, beforeEach } from "vitest";

/** ★★★ **설정 화면이 앱의 나머지와 같은 물건으로 보여야 한다.**
 *
 * 2026-09-01 사용자 지적(화면 갈무리): 볼트를 추가·설정하는 화면이 "너무 구리다".
 * 실제로 셋이 겹쳐 있었다:
 *
 *   ① 탭 줄에 **`볼트` 가 두 번** 나온다 — 하나는 볼트 화면으로 돌아가는 문이고
 *      하나는 볼트 설정 탭이다. 같은 낱말이 두 가지를 가리킨다.
 *   ② 본문이 위에서 아래로 흐르는 폼 덩어리다. 볼트 카드와 "새 볼트 만들기" 가
 *      경계 없이 이어져서 어디까지가 한 볼트인지 안 보인다.
 *   ③ `프로젝트 → 볼트` 가 **열일곱 줄 전부 `default (기본)`** 이다. 고를 것이
 *      하나뿐인데 열일곱 개의 선택 상자를 그렸다 — 정보가 0인 화면이다.
 *
 * 앱의 주역(볼트 브라우저)은 이미 사이드바 + 본문 두 칸이다. 설정만 옛 탭 구조로
 * 남아 있어서 다른 앱처럼 보였다. */

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

const ROW = {
  stem: "common-결정-a-2026-08-01", path: "/v/a.md", rel: "a.md", vault: "default",
  domain: ["common"], date: "2026-08-01", status: "active", outcome: "pending",
  summary: "무언가 골랐다", tags: ["decision"],
};

const DOMAINS = ["common", "editup", "priorcase", "tutela", "bard", "create",
  "draft.ai", "mesh", "nova", "novels", "platform", "omni", "synth",
  "OCC", "영상제작", "twincrew", "lotte"].map((prefix) => ({ prefix, folder: prefix, vault: "" }));

function settings(vaults: unknown[]): unknown {
  return { vault_parent: "/p", vaults, domains: DOMAINS, hosts: [] };
}

const ONE = [{
  name: "default", path: "/Users/x/Obsidian Vault", exists: true,
  decisions: 629, domains: DOMAINS.map((d) => d.prefix), remote: "https://github.com/x/v.git",
}];

const TWO = [
  ONE[0],
  { name: "회사", path: "/Users/x/work-vault", exists: true, decisions: 0, domains: [], remote: "" },
];

function wire(vaults: unknown[]): void {
  invoke.mockImplementation((cmd: string) => {
    if (cmd === "list_notes") return Promise.resolve(JSON.stringify([ROW]));
    if (cmd === "show_note") return Promise.resolve(JSON.stringify(ROW));
    if (cmd === "queue") return Promise.resolve(JSON.stringify({ confirm: [], review: [], retro: [], health: [] }));
    if (cmd === "settings") return Promise.resolve(JSON.stringify(settings(vaults)));
    return Promise.resolve(undefined);
  });
}

async function settle(): Promise<void> {
  for (let i = 0; i < 30; i++) await Promise.resolve();
  await new Promise((r) => setTimeout(r, 0));
  for (let i = 0; i < 30; i++) await Promise.resolve();
}

/** openSettings 는 사람이 실제로 가는 길로 간다 — 사이드바 맨 아래 톱니바퀴. */
async function openSettings(vaults: unknown[] = ONE): Promise<void> {
  document.body.innerHTML = '<div id="app"></div>';
  invoke.mockReset();
  wire(vaults);
  vi.resetModules();
  await import("../src/main");
  await settle();
  document.querySelector<HTMLButtonElement>(".side-settings")!.click();
  await settle();
}

/** openVaultTab 은 설정을 열고 **볼트 칸까지** 간다. 기본 칸은 호스트다. */
async function openVaultTab(vaults: unknown[] = ONE): Promise<void> {
  await openSettings(vaults);
  const item = [...document.querySelectorAll<HTMLButtonElement>(".side-item")]
    .find((b) => b.textContent?.trim() === "볼트");
  expect(item, "사이드바에 볼트 항목이 없다").toBeTruthy();
  item!.click();
  await settle();
}

const texts = (sel: string): string[] =>
  [...document.querySelectorAll(sel)].map((n) => (n.textContent ?? "").trim());

describe("설정 화면의 뼈대", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  // ★ 볼트 화면과 **같은 두 칸**이다. 설정만 다른 골격이면 다른 앱처럼 보인다.
  it("사이드바와 본문 두 칸이다 — 볼트 화면과 같은 골격", async () => {
    await openSettings();
    expect(document.querySelector(".pane2"), "두 칸 격자가 없다").toBeTruthy();
    expect(document.querySelector(".side"), "사이드바가 없다").toBeTruthy();
    expect(document.querySelector(".tabs"), "옛 탭 줄이 남아 있다").toBeFalsy();
  });

  // ★★ 지적된 고장: 같은 낱말이 두 가지를 가리킨다.
  it("`볼트` 라는 이름이 두 번 나오지 않는다", async () => {
    await openSettings();
    const nav = texts(".side-item, .side-back");
    const 볼트 = nav.filter((t) => t === "볼트");
    expect(볼트.length, `사이드바 항목: ${JSON.stringify(nav)}`).toBe(1);
  });

  it("돌아가는 길이 있다", async () => {
    await openSettings();
    const back = document.querySelector<HTMLButtonElement>(".side-back");
    expect(back, "설정에서 나갈 길이 없다").toBeTruthy();
    back!.click();
    await settle();
    expect(document.querySelector(".list-row"), "볼트 화면으로 안 돌아왔다").toBeTruthy();
  });
});

describe("프로젝트 → 볼트", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  // ★★ 지적된 고장: 열일곱 줄이 전부 `default (기본)` 이었다.
  it("볼트가 하나면 선택 상자를 안 만든다 — 고를 것이 없다", async () => {
    await openVaultTab(ONE);
    expect(document.querySelectorAll("select").length,
      "고를 곳이 하나뿐인데 선택 상자를 그렸다").toBe(0);
    // 그래도 **어느 프로젝트가 있는지는 보여야 한다** — 이 화면의 존재 이유다.
    const shown = document.body.textContent ?? "";
    for (const p of ["common", "priorcase", "lotte"]) {
      expect(shown, `${p} 가 안 보인다`).toContain(p);
    }
  });

  it("볼트가 둘이면 고를 수 있다", async () => {
    await openVaultTab(TWO);
    const sels = document.querySelectorAll<HTMLSelectElement>("select");
    expect(sels.length, "볼트가 둘인데 못 고른다").toBe(DOMAINS.length);
    expect([...sels[0].options].map((o) => o.value)).toEqual(["default", "회사"]);
  });

  // ★ 볼트를 만들었는데 아무 프로젝트도 안 쓰는 상태는 **조용하다.** 기록은 전부
  // 옛 볼트로 계속 가는데 화면은 멀쩡해 보인다 (renderVaults 의 §).
  it("아무 프로젝트도 안 쓰는 볼트는 눈에 띈다", async () => {
    await openVaultTab(TWO);
    const card = [...document.querySelectorAll<HTMLElement>(".vault-card")]
      .find((c) => c.textContent?.includes("회사"));
    expect(card, "회사 볼트 카드가 없다").toBeTruthy();
    expect(card!.textContent, "안 쓰이는 볼트인 것이 안 보인다").toMatch(/아무 프로젝트도|쓰는 프로젝트가 없/);
  });
});

// ── 낡은 판·이상한 데이터에도 화면이 안 사라진다 ──────────────────────
//
// 2026-09-01 사고: 볼트 `회사` 를 만들었더니 **볼트 화면이 통째로 사라졌다.**
// 새 볼트의 `domains` 가 JSON `null` 로 왔고(Go 의 nil 슬라이스), `v.domains.length`
// 가 TypeError 를 냈다. 그 예외가 렌더를 끊어 뒤쪽(새 볼트 폼·프로젝트 목록)이
// 전부 안 그려졌고, 새로고침해도 같은 자리에서 또 터지니 영구적으로 보였다.
//
// Go 쪽은 고쳤다. 그래도 여기 방어가 필요한 이유는 **앱에 번들된 prior 가 낡을
// 수 있어서**다 — 검은 화면 사고의 원인 ①이 정확히 그것이었다. 그리고 설정
// 화면에는 볼트 화면과 달리 **오류 경계가 없었다.**

const NULLISH = [
  { name: "default", path: "/v", exists: true, decisions: 3, domains: ["common"], remote: "" },
  // 낡은 판이 내는 모양 — 빈 목록이 null 로 온다.
  { name: "회사", path: "/v2", exists: true, decisions: 0, domains: null, remote: null },
];

describe("망가진 데이터", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  // ★★★ 지적된 고장 그대로.
  it("domains 가 null 인 볼트가 있어도 화면이 안 사라진다", async () => {
    await openVaultTab(NULLISH as unknown[]);

    // 두 볼트가 다 보여야 한다.
    const cards = document.querySelectorAll(".vault-card");
    expect(cards.length, "볼트 카드가 모자란다").toBe(2);
    // **그리고 그 뒤가 살아 있어야 한다.** 사라진 것이 정확히 이 둘이었다.
    expect(document.querySelector(".vault-add"), "새 볼트 폼이 사라졌다").toBeTruthy();
    expect(document.body.textContent, "프로젝트 섹션이 사라졌다").toContain("프로젝트 → 볼트");
  });

  // ★★ 설정 화면에도 오류 경계가 있어야 한다.
  //
  // 방어를 아무리 넣어도 **다음에 어디서 터질지는 모른다.** 볼트 화면은 그것을
  // 인정하고 그물을 쳤는데(2026-09-01 검은 화면) 설정 화면은 안 쳤다. 터졌을 때
  // 최소한 무엇이 터졌는지는 보여야 한다.
  it("설정을 그리다 터져도 빈 화면이 되지 않는다", async () => {
    // **방어를 뚫는 입력이어야 한다.** 배열이 아닌 것은 이제 빈 목록으로 읽히므로
    // 안 터진다 — 그건 방어가 도는 것이지 경계가 도는 것이 아니다. 배열이되
    // 원소가 없는 값이면 어떤 방어도 안 거치고 속성 읽기에서 터진다.
    await openVaultTab([null] as unknown as unknown[]);
    const shown = document.body.textContent ?? "";
    expect(shown.trim(), "화면이 통째로 비었다").not.toBe("");
    expect(shown, "무엇이 터졌는지 안 보인다").toMatch(/멈췄|실패|오류/);
  });
});
