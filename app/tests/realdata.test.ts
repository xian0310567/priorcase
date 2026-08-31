import { describe, it, expect, beforeAll } from "vitest";
import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";
import { renderHosts } from "../src/render/hosts";
import { renderVaults } from "../src/render/vaults";
import { renderHealth } from "../src/render/health";
import { renderWarnings } from "../src/render/shell";
import { backlogLine } from "../src/format";
import type { Queue, Settings } from "../src/types";

// ★★★ **진짜 기계의 설정과 큐를 화면 함수에 통과시킨다.**
//
// 합성 픽스처는 내가 상상한 모양만 담는다. 실제 데이터는 다르다 — 도메인 10개
// 중 셋은 볼트 폴더가 아직 없고, 호스트 하나는 대화가 1,729개이며, 도메인
// 이름에 한글이 있다. 그런 모양은 픽스처를 쓸 때 떠올리지 못한 것들이고,
// **호스트 파서에서도 합성 픽스처가 못 잡은 결함 둘을 실측 데이터가 잡았다.**
//
// prior 가 없는 기계에서는 건너뛴다 — 이 시험은 여기 있는 것이 값이지,
// 어디서나 도는 것이 값이 아니다.

const PRIOR = join(homedir(), ".local", "bin", "prior");
const installed = existsSync(PRIOR);

function readJSON<T>(args: string[]): T {
  return JSON.parse(
    execFileSync(PRIOR, args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 }),
  ) as T;
}

/** fresh 는 설치된 prior 가 이 앱이 부르는 명령을 아는지 본다.
 *
 * **낡은 것을 "없는 것" 으로 뭉개면 안 된다.** 그러면 이 시험이 영영 건너뛰기만
 * 하면서 통과한 것처럼 보인다 — 실측 데이터로 지키려던 것을 아무것도 안 지킨다.
 * 없으면 건너뛰고, 있는데 낡았으면 아래에서 실패한다. */
const fresh =
  installed &&
  (() => {
    try {
      execFileSync(PRIOR, ["settings", "--json"], { encoding: "utf8", stdio: "pipe" });
      return true;
    } catch {
      return false;
    }
  })();

// ★★★ 설치된 prior 가 낡았으면 **조용히 건너뛰지 않는다.**
describe.skipIf(!installed)("설치된 prior", () => {
  it("앱이 부르는 명령을 안다", () => {
    expect(
      fresh,
      `${PRIOR} 가 prior settings 를 모른다 — 다시 설치해라: ` +
        "go build -o ~/.local/bin/prior ./cmd/prior",
    ).toBe(true);
  });
});

let q: Queue;
let s: Settings;
beforeAll(() => {
  if (!fresh) return;
  q = readJSON<Queue>(["queue", "--json"]);
  s = readJSON<Settings>(["settings", "--json"]);
});

const noop = {
  toggle: () => {},
  open: () => {},
  add: () => {},
  bind: () => {},
      remote: () => {},
};

describe.skipIf(!fresh)("진짜 데이터로 화면을 그린다", () => {
  it("설정이 계약을 지킨다", () => {
    for (const k of ["vaults", "domains", "hosts"] as const) {
      expect(Array.isArray(s[k]), `${k} 가 배열이 아니다`).toBe(true);
    }
    expect(s.config_path, "설정 경로가 비었다").not.toBe("");
    // **호스트 목록의 정본은 레지스트리다.** 설정에 한 줄도 안 적어도 전부 나와야
    // 한다 — 안 그러면 새 파서를 붙였을 때 사람이 그 존재를 영영 모른다.
    expect(s.hosts.length, "호스트가 없다").toBeGreaterThan(0);
    for (const h of s.hosts) {
      expect(typeof h.enabled, `${h.name}: enabled 가 불리언이 아니다`).toBe("boolean");
      expect(typeof h.files, `${h.name}: files 가 숫자가 아니다`).toBe("number");
    }
    // 볼트마다 그것을 쓰는 도메인 목록이 붙어야 한다 (null 이면 .length 가 죽는다).
    for (const v of s.vaults) {
      expect(Array.isArray(v.domains), `볼트 ${v.name}: domains 가 배열이 아니다`).toBe(true);
    }
  });

  it("호스트 화면이 그려진다", () => {
    const root = document.createElement("div");
    renderHosts(root, s.hosts, noop);
    expect(root.querySelectorAll(".host-row").length).toBe(s.hosts.length);
    // 체크 상태가 설정과 같아야 한다 — 어긋나면 껐는데 켜져 보인다.
    const boxes = root.querySelectorAll<HTMLInputElement>("input.host-toggle");
    for (const [i, h] of s.hosts.entries()) {
      expect(boxes[i].checked, `${h.name}: 체크 상태가 설정과 다르다`).toBe(h.enabled);
    }
  });

  it("볼트 화면이 그려진다", () => {
    const root = document.createElement("div");
    renderVaults(root, s, noop);
    expect(root.querySelectorAll(".vault-row").length).toBe(s.vaults.length);
    expect(root.querySelectorAll(".domain-row").length).toBe(s.domains.length);
    // 도메인마다 고른 볼트가 하나씩 있어야 한다. 빈 선택은 "어디로 가는지 모름"
    // 으로 보이는데, 실제로는 기본 볼트로 잘 가고 있다.
    for (const sel of root.querySelectorAll<HTMLSelectElement>("select.domain-vault")) {
      expect(sel.value, "볼트가 안 골라진 도메인이 있다").not.toBe("");
    }
  });

  it("자리가 없는 볼트는 열기가 막힌다", () => {
    const root = document.createElement("div");
    renderVaults(root, s, noop);
    const rows = root.querySelectorAll(".vault-row");
    for (const [i, v] of s.vaults.entries()) {
      const btn = rows[i].querySelector<HTMLButtonElement>("button.btn")!;
      expect(btn.disabled, `볼트 ${v.name}: exists=${v.exists} 인데 버튼 상태가 다르다`).toBe(
        !v.exists,
      );
    }
  });

  it("상태가 그려지고 모르는 등급이 없다", () => {
    const root = document.createElement("div");
    renderHealth(root, q.health, backlogLine(q.confirm.length, q.retro.length));
    expect(root.querySelectorAll(".health-row").length).toBe(q.health.length);
    // 실제 prior 가 내는 등급은 계약 안에 있어야 한다. unknown 이 나오면
    // Go 쪽에 등급이 늘었는데 타입을 안 고친 것이다.
    expect(root.querySelectorAll(".level-unknown").length, "모르는 등급이 왔다").toBe(0);
  });

  it("경고가 있으면 배너가 뜬다", () => {
    const root = document.createElement("div");
    renderWarnings(root, [...(q.warnings ?? []), ...(s.warnings ?? [])]);
    const any = (q.warnings?.length ?? 0) + (s.warnings?.length ?? 0) > 0;
    if (any) {
      expect(root.textContent).toContain("불완전");
    } else {
      expect(root.innerHTML).toBe("");
    }
  });

  // ★★ **남의 글이 화면 구조를 못 뒤튼다.** 볼트 경로와 도메인 이름은 사람이
  //    설정 파일에 적은 것이고, 거기엔 무엇이든 들어갈 수 있다.
  it("설정 값이 요소를 만들지 않는다", () => {
    const root = document.createElement("div");
    renderVaults(root, s, noop);
    const allowed = new Set(["DIV", "SPAN", "BUTTON", "P", "H3", "INPUT", "SELECT", "OPTION"]);
    const bad = Array.from(root.querySelectorAll("*")).filter((n) => !allowed.has(n.tagName));
    expect(
      bad.map((n) => n.tagName),
      "설정 값에서 온 태그가 있다",
    ).toEqual([]);
  });
});
