import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const raw = readFileSync(resolve(process.cwd(), "src/main.ts"), "utf8");

/** 주석을 벗긴 본문. **주석은 과거를 적는 자리다** — 이 파일의 주석에는 무엇이
 * 왜 틀렸는지 보이려고 옛 코드가 그대로 인용돼 있어서, 안 벗기면 "옛 모양이
 * 남아 있다" 로 잘못 걸린다 (style.css 의 팔레트 검사와 같은 면제). */
const main = raw.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/.*$/gm, "");

/** ★ 앱은 **빠른 것부터 그린다.**
 *
 * 2026-09-01 실측(결정 560건): `prior settings --json` 0.056초 · `prior queue --json`
 * 6.3초. 예전에는 Promise.all 로 둘을 같이 기다려서, 호스트 탭만 보려 해도 6.3초가
 * 걸렸다 — 그 탭이 쓰는 것은 settings 뿐인데.
 *
 * queue 비용은 **볼트 크기에 비례해 자란다.** 빠른 쪽을 느린 쪽에 묶어 두면 그
 * 성장이 앱 전체의 체감이 되므로, 이 분리는 지금의 최적화보다 오래간다.
 */
describe("단계적 로딩", () => {
  it("settings 와 queue 를 같이 기다리지 않는다", () => {
    expect(main).not.toMatch(/Promise\.all\(\s*\[\s*fetchQueue\(\)\s*,\s*fetchSettings\(\)/);
  });

  it("settings 를 먼저 받아 바로 그린다", () => {
    // settings 를 await 한 뒤 queue 를 await 하기 전에 draw 가 한 번 있어야 한다.
    const iSettings = main.indexOf("await fetchSettings()");
    const iDrawFirst = main.indexOf("draw(lastQ, s)", iSettings);
    const iQueue = main.indexOf("await fetchQueue()", iSettings);
    expect(iSettings, "fetchSettings 호출이 없다").toBeGreaterThan(-1);
    expect(iDrawFirst, "settings 뒤에 곧바로 그리는 draw 가 없다").toBeGreaterThan(-1);
    expect(iQueue, "fetchQueue 호출이 없다").toBeGreaterThan(-1);
    expect(iDrawFirst).toBeLessThan(iQueue);
  });

  it("큐가 없어도 그릴 수 있게 draw 가 null 을 받는다", () => {
    expect(main).toMatch(/function draw\(q: Queue \| null, s: Settings\)/);
  });

  it("설정 실패는 화면 전체 오류로 남는다", () => {
    // 옛 주석의 걱정: "설정이 안 읽히는데 상태만 그리면 사람은 앱이 멀쩡한 줄 안다."
    // 큐만 실패할 때와 달라야 한다.
    const block = main.slice(main.indexOf("s = await fetchSettings()"));
    expect(block.slice(0, 300)).toMatch(/catch[\s\S]*shell\(\)[\s\S]*renderError/);
  });

  it("큐 실패는 상태 탭에서만 말한다", () => {
    expect(main).toMatch(/queueErr\s*=\s*e as CmdError/);
    expect(main).toMatch(/if \(queueErr\)\s*\{\s*renderError\(body, queueErr\)/);
  });

  it("두 번째 폴링부터는 '읽는 중' 으로 되돌아가지 않는다", () => {
    // lastQ 를 들고 있어야 한다. 없으면 60초마다 상태 탭이 로딩으로 깜빡인다.
    expect(main).toMatch(/let lastQ: Queue \| null = null/);
    expect(main).toMatch(/lastQ = q/);
  });
});
