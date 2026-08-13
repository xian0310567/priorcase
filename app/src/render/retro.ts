import type { Retro } from "../types";
import { vaultLabel, reasonLabel } from "../format";
import { renderEmpty } from "./shell";
import { el } from "./excerpt";

export interface RetroActions {
  judge(stem: string, outcome: "good" | "bad"): void;
  /** 노트를 연다 — 요약 한 줄로는 "그 결정이 좋았나" 를 판정할 수 없다. */
  open(stem: string): void;
}

/** dismissed 는 이번에 미뤄 둔 결정들이다.
 *
 * **앱이 떠 있는 동안만 기억한다.** 파일에 남기면 "왜 안 뜨지" 를 설명할 규칙이
 * 둘이 되고, 되살릴 방법이 없어진다. 앱을 다시 켜면 다시 뜬다 — 미룸은 지금
 * 이 순간 안 보고 싶다는 뜻이지 영영 안 보겠다는 뜻이 아니다.
 *
 * 왜 필요한가: 앱은 30초마다 큐를 다시 받아 다시 그린다. 카드를 DOM 에서만
 * 지우면 **30초 뒤 그대로 돌아온다.** 실측(2026-08-13)으로 회고 큐가 50건이라,
 * 미룬 것이 계속 되살아나면 사람은 그 화면을 통째로 포기한다.
 */
const dismissed = new Set<string>();

/** resetDismissed 는 미룬 목록을 비운다. 시험이 서로 새지 않게 하는 문이다. */
export function resetDismissed(): void {
  dismissed.clear();
}

export function renderRetro(root: HTMLElement, items: Retro[], on: RetroActions): void {
  root.replaceChildren();
  const live = items.filter((it) => !dismissed.has(it.stem));
  if (live.length === 0) {
    renderEmpty(root, "회고할 결정");
    return;
  }
  for (const it of live) {
    const card = el("div", "card");

    const head = el("div", "card-head");
    head.append(
      el("span", "when", it.date),
      el("span", "vault", vaultLabel(it.domain, it.vault)),
      // superseded 는 hits 가 0 일 수 있다 — "재회수 0회" 로 그리면 거짓이다.
      el("span", "reason", reasonLabel(it.reason, it.hits)),
    );
    card.append(head);

    card.append(el("div", "summary", it.summary));
    // **어느 노트인지 보여야 한다.** 요약만으로는 비슷한 결정 둘을 구별할 수
    // 없고, 실측으로 회고 큐가 50건이다 (priorcase 만 24건).
    card.append(el("div", "stem", it.stem));
    if (it.author) card.append(el("div", "author", `— ${it.author}`));

    const btns = el("div", "buttons");
    const good = el("button", "primary", "좋았다") as HTMLButtonElement;
    good.addEventListener("click", () => on.judge(it.stem, "good"));
    const bad = el("button", "secondary", "나빴다") as HTMLButtonElement;
    bad.addEventListener("click", () => on.judge(it.stem, "bad"));
    // **[아직] 은 아무 명령도 부르지 않는다.** 같은 방아쇠가 한 번 더 울릴
    // 때까지 안 묻는 것이 규칙이고, 그건 재회수 카운트가 오르면 자연히 된다.
    // 볼트에 상태를 남기면 규칙이 둘이 되고 "왜 안 뜨지" 를 설명할 수 없다.
    const later = el("button", "secondary", "아직") as HTMLButtonElement;
    later.addEventListener("click", () => {
      dismissed.add(it.stem);
      renderRetro(root, items, on);
    });
    // **열어 볼 수 있어야 한다.** 요약 한 줄(30~139자)로 "그 결정이 결과적으로
    // 좋았나" 를 판정하는 것은 확인이 아니라 짐작이다. 근거를 다시 읽을 문을 둔다.
    const open = el("button", "secondary", "노트 열기") as HTMLButtonElement;
    open.addEventListener("click", () => on.open(it.stem));
    btns.append(good, bad, open, later);
    card.append(btns);

    root.append(card);
  }
}
