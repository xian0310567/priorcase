import type { Health, Level } from "../types";
import { el } from "./shell";

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
 * backlog 는 밀린 일감을 적은 **진단 한 줄**이다. 빈 문자열이면 안 그린다.
 *
 * **할 일 목록이 아니다.** 사람이 누를 것은 없다 — 밀린 구간은 데몬이 세션
 * 끝마다 소화한다. 그래도 적는 이유는, 그 처리량이 새 구간이 쌓이는 속도를 못
 * 따라가면 사람이 그 사실을 알 자리가 아무 데도 없기 때문이다. 예전에는 이것이
 * 확인 큐라는 화면이었고, 그건 자동 기록의 전제를 사람에게 떠넘기는 것이었다. */
export function renderHealth(root: HTMLElement, items: Health[], backlog = ""): void {
  root.replaceChildren();
  if (items.length === 0) {
    root.append(el("p", "empty", "상태 검사를 받지 못했다. prior 가 도는지 확인하라."));
    return;
  }
  if (backlog !== "") {
    root.append(el("div", "backlog", backlog));
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
