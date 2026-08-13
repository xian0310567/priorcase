import type { Health, Level } from "../types";
import { el } from "./excerpt";

const MARK: Record<Level, string> = { ok: "✓", warn: "⚠", fail: "✗", unknown: "?" };

/** levelOf 는 등급을 계약 안의 값으로 좁힌다.
 *
 * **모르는 값을 ok 로 뭉개면 안 된다.** TS 타입은 런타임 JSON 을 검사하지
 * 않으므로 Go 쪽에 등급이 하나 늘면 그대로 흘러 들어온다. 그때 정상으로 그리면
 * **하필 알아야 할 상태가 안 보인다** — 이 프로젝트가 죄목으로 드는 침묵이다. */
function levelOf(v: string): Level {
  return v === "ok" || v === "warn" || v === "fail" ? v : "unknown";
}

/** renderHealth 는 상태 검사를 그린다.
 *
 * **큐가 셋 다 비었을 때 앱이 보여 줄 것이 이것뿐이다.** 비어 있어도 반드시
 * 채운다 — 빈 화면은 "고장난 것처럼" 보인다. */
export function renderHealth(root: HTMLElement, items: Health[]): void {
  root.replaceChildren();
  if (items.length === 0) {
    root.append(el("p", "empty", "상태 검사를 받지 못했다. prior 가 도는지 확인하라."));
    return;
  }
  for (const it of items) {
    const lvl = levelOf(it.level);
    const row = el("div", `health-row level-${lvl}`);
    row.append(
      el("span", "health-mark", MARK[lvl]),
      el("span", "health-name", it.name),
      el("span", "health-detail", it.detail),
    );
    // **등급으로 fix 를 감추지 않는다.** ok 인데 fix 가 오는 판이 있다
    // (실측: "도메인 폴더" 는 ok 이면서 folder 값을 확인하라고 안내한다).
    if (it.fix) row.append(el("div", "health-fix", `→ ${it.fix}`));
    root.append(row);
  }
}
