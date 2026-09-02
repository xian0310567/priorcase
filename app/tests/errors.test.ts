import { describe, it, expect, beforeEach, vi } from "vitest";
import { execFileSync } from "node:child_process";
import { join } from "node:path";
import { renderError, renderWarnings } from "../src/render/shell";
import { renderHealth } from "../src/render/health";
import type { Queue, Settings, CmdError } from "../src/types";

// 스펙 §6 오류 처리 표의 여섯 행이 그대로 여섯 시험이다.
// **빈 큐와 실패를 구별하는 것이 이 표의 전부다.** 이 시스템은 모든 부품이
// 실패해도 대화를 막지 않도록 설계됐고, 그 대가로 고장이 정상과 구별되지
// 않는다 — 앱이 그 구별을 못 하면 앱도 같은 병에 걸린다.

const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));
const { fetchQueue } = await import("../src/api");
const { badgeText } = await import("../src/main");

/** fixture 는 가짜 prior 를 **실제로 돌려** 그 stdout 을 준다.
 *
 * **JSON 을 시험에 베껴 넣지 않는다.** 베끼면 픽스처와 시험이 각자 진화하고,
 * 어긋나도 아무도 모른다 — 그러면 손으로 확인할 때 쓰는 픽스처가 시험이
 * 지키는 것과 다른 물건이 된다.
 *
 * **`bash` 를 명시적으로 태운다.** 스크립트를 직접 spawn 하면 윈도우에서
 * `spawnSync … EFTYPE` 로 죽는다 — 윈도우는 shebang 을 모르고 `.sh` 를 실행
 * 파일로 안 친다. 2026-09-02 v0.5.0 릴리스에서 이것 때문에 윈도우 앱 빌드가
 * 통째로 실패했고(8건), 맥·리눅스에서만 돌던 동안에는 안 드러났다. */
function fixture(name: string, args: string[] = []): string {
  const script = join(__dirname, "..", "fixtures", `fake-prior-${name}.sh`);
  return execFileSync("bash", [script, ...args], { encoding: "utf8" });
}

let root: HTMLElement;
beforeEach(() => {
  root = document.createElement("div");
  invoke.mockReset();
});

describe("스펙 §6 오류 처리", () => {
  it("① prior 를 못 찾으면 설치 안내만 보여 준다", () => {
    renderError(root, { kind: "not_found", message: "prior 를 찾을 수 없다" });
    expect(root.textContent).toContain("설치");
    expect(root.textContent).not.toContain("할 일이 없");
  });

  it("② 0 아닌 종료는 stderr 를 그대로 보여 준다", () => {
    const e: CmdError = { kind: "failed", message: "설정에 알 수 없는 키가 있다 (/x.toml)" };
    renderError(root, e);
    expect(root.textContent).toContain("/x.toml");
  });

  it("③ warnings 가 있으면 배너를 낸다", () => {
    const q: Queue = JSON.parse(fixture("warnings", ["queue", "--json"]));
    renderWarnings(root, q.warnings);
    expect(root.textContent).toContain("불완전");
    expect(root.textContent).toContain("볼트 work");
  });

  it("④ 쓰기 실패는 이유를 보여 준다", () => {
    renderError(root, { kind: "failed", message: "승격이 일어나지 않았다 (/t.jsonl@0)" });
    expect(root.textContent).toContain("승격이 일어나지 않았다");
  });

  // ★★★ 볼트 하나가 깨져도 **나머지 볼트의 큐는 그대로 보여 준다.**
  //
  // 하나가 깨졌다고 전체를 오류 화면으로 덮으면, 할 수 있는 일까지 못 하게
  // 된다. 경고로 알리되 살아 있는 것은 그린다.
  it("⑤ 볼트 하나가 깨져도 나머지를 그린다", () => {
    const q: Queue = JSON.parse(fixture("onebroken", ["queue", "--json"]));
    const banner = document.createElement("div");
    renderWarnings(banner, q.warnings);
    expect(banner.textContent).toContain("불완전");

    // 깨진 볼트는 상태에서 fail 로 드러나야 한다. **오류 화면으로 덮지 않는다** —
    // 하나가 깨졌다고 전체를 오류로 만들면 할 수 있는 일까지 못 하게 된다.
    const health = document.createElement("div");
    renderHealth(health, q.health);
    expect(health.querySelector(".level-fail")).not.toBeNull();
    // 살아 있는 검사도 같이 보여야 한다.
    expect(health.querySelectorAll(".health-row").length).toBeGreaterThan(1);
    // 메뉴바도 고장을 알린다.
    expect(badgeText(q)).toBe("⚠");
  });

  // ★★★ **깨진 JSON 은 빈 큐가 아니다.**
  //
  // 가짜 prior 가 내는 **실제 바이트**를 fetchQueue 에 흘려서 확인한다 —
  // 손으로 만든 문자열로 하면 픽스처가 실제로 무엇을 내는지는 안 본 셈이다.
  it("⑥ 깨진 JSON 은 빈 큐가 아니라 오류다", async () => {
    const raw = fixture("broken-json", ["queue", "--json"]);
    expect(() => JSON.parse(raw), "픽스처가 실제로 깨진 JSON 을 내야 한다").toThrow();

    invoke.mockResolvedValue(raw);
    const e = await fetchQueue().then(
      () => null,
      (x) => x as CmdError,
    );
    expect(e, "빈 큐로 뭉갰다").not.toBeNull();
    expect(e!.kind).toBe("failed");

    renderError(root, e!);
    expect(root.textContent).toContain("JSON 이 아닌");
    expect(root.textContent).not.toContain("할 일이 없");
  });
});

describe("빈 상태는 고장처럼 보이지 않는다", () => {
  it("큐가 셋 다 비어도 상태가 채워진다", () => {
    const q: Queue = JSON.parse(fixture("ok", ["queue", "--json"]));
    renderHealth(root, q.health);
    expect(root.textContent!.length, "상태 화면이 비었다").toBeGreaterThan(0);
    expect(root.textContent).not.toContain("상태 검사를 받지 못했다");
  });

  // ★★ 정상이면서 할 일이 없으면 **메뉴바에 글자가 없어야 한다.**
  //    "0" 이나 "✓" 가 늘 떠 있으면 그것도 상시 신호가 되어 무시하게 된다.
  it("정상이고 할 일이 없으면 메뉴바가 조용하다", () => {
    const q: Queue = JSON.parse(fixture("ok", ["queue", "--json"]));
    expect(badgeText(q)).toBe("");
  });

  // ★★ warnings 만 있고 큐가 비면 — **배지는 조용하지만 배너는 뜬다.**
  //    이게 "빈 큐" 와 "불완전한 큐" 를 가르는 자리다.
  it("경고만 있으면 배지는 조용하고 배너가 뜬다", () => {
    const q: Queue = JSON.parse(fixture("warnings", ["queue", "--json"]));
    expect(badgeText(q)).toBe("");
    renderWarnings(root, q.warnings);
    expect(root.textContent).toContain("불완전");
  });
});

describe("가짜 prior 픽스처", () => {
  // ★★ 손으로 확인할 때 쓰는 픽스처가 **시험이 지키는 것과 같은 물건**이어야
  //    한다. 계약을 어기는 픽스처로 눈 확인을 하면 통과한 것이 거짓이 된다.
  it("JSON 을 내는 픽스처는 큐 계약을 지킨다", () => {
    for (const name of ["ok", "warnings", "onebroken"]) {
      const q = JSON.parse(fixture(name, ["queue", "--json"])) as Queue;
      for (const k of ["confirm", "review", "retro", "health"] as const) {
        expect(Array.isArray(q[k]), `${name}: ${k} 가 배열이 아니다`).toBe(true);
      }
    }
  });

  // ★★★ **픽스처가 명령마다 다르게 답해야 한다.**
  //
  // 앱은 queue 와 settings 를 함께 읽는다. 한 가지만 내는 픽스처로 손 확인을
  // 하면 설정 화면이 빈 객체를 받아 통째로 죽는데, 그건 그 픽스처가 재현하려던
  // 상태가 아니다 — **틀린 것을 보면서 통과했다고 여기게 된다.**
  it("픽스처가 settings 계약도 지킨다", () => {
    for (const name of ["ok", "warnings", "onebroken"]) {
      const s = JSON.parse(fixture(name, ["settings", "--json"])) as Settings;
      expect(s.config_path, `${name}: 설정 경로가 없다`).toBeTruthy();
      for (const k of ["vaults", "domains", "hosts"] as const) {
        expect(Array.isArray(s[k]), `${name}: ${k} 가 배열이 아니다`).toBe(true);
      }
      expect(s.hosts.length, `${name}: 호스트가 없다`).toBeGreaterThan(0);
    }
  });

  it("깨진 JSON 픽스처는 정말로 깨져 있다", () => {
    expect(() => JSON.parse(fixture("broken-json", ["queue", "--json"]))).toThrow();
  });
});
