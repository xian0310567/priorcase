import type { Confirm } from "../types";
import { vaultLabel } from "../format";
import { renderEmpty } from "./shell";
import { el, mountExcerpt } from "./excerpt";

export interface ConfirmActions {
  resolve(id: string): void;
  promote(id: string): void;
}

/** EXCERPT_LINES 는 **접었을 때** 보이는 줄 수다.
 *
 * 실측(2026-08-13, 확인 23건)에서 발췌는 29~113줄이고 **23건 전부** 이 값을
 * 넘는다. 즉 이건 "가끔 긴 것을 접는다" 가 아니라 **언제나 접힌다**는 뜻이다.
 * 그래서 펼치기가 장식이 아니라 이 화면의 필수 부품이다 — 접힌 8줄로 "결정인가"
 * 를 판단하는 건 확인이 아니라 판별기를 믿는 것이다. */
const EXCERPT_LINES = 8;

export function renderConfirm(root: HTMLElement, items: Confirm[], on: ConfirmActions): void {
  root.replaceChildren();
  if (items.length === 0) {
    renderEmpty(root, "확인할 구간");
    return;
  }
  for (const it of items) {
    const card = el("div", "card");

    // 머리: 언제 · 어느 프로젝트 · 어느 볼트 · 시그널
    const head = el("div", "card-head");
    head.append(el("span", "when", it.when), el("span", "vault", vaultLabel(it.domain, it.vault)));
    if (it.signals.length > 0) {
      head.append(el("span", "signals", `시그널 ${it.signals.join("·")}`));
    }
    // **판별기 실패는 반드시 드러낸다.** 포기한 줄은 자동으로 다시 안 온다 —
    // 그 사실을 안 보여 주면 사람은 곧 처리되겠거니 하고 영영 기다린다.
    if (it.gave_up) {
      head.append(el("span", "gaveup", `⚠️ 자동 처리 포기 (판별기 ${it.fails}회 실패)`));
    } else if (it.fails > 0) {
      head.append(el("span", "fails", `판별기 ${it.fails}회 실패`));
    }
    card.append(head);

    // 비슷한 기존 결정 — **점수와 함께, 단정 없이.**
    //
    // 회수는 언제나 무언가를 돌려주므로 일치가 없는 발췌도 1위가 나온다
    // (실측: 진짜 일치 65점과 가짜 1위 54점이 겹쳤다). "이미 기록됨" 같은
    // 문구를 쓰면 사람이 읽지 않고 지운다.
    if (it.similar.length > 0) {
      const box = el("div", "similar");
      box.append(el("div", "similar-title", "비슷한 기존 결정"));
      for (const s of it.similar) {
        const row = el("div", "similar-row");
        row.append(el("span", "score", String(s.score)), el("span", "stem", s.stem));
        if (s.summary) row.append(el("div", "similar-summary", s.summary));
        box.append(row);
      }
      card.append(box);
    }

    mountExcerpt(card, it.excerpt, EXCERPT_LINES);

    // 버튼 둘
    const btns = el("div", "buttons");
    const yes = el("button", "primary", "결정이다 → 기록한다") as HTMLButtonElement;
    yes.addEventListener("click", () => on.promote(it.id));
    const no = el("button", "secondary", "아니다 → 지운다") as HTMLButtonElement;
    no.addEventListener("click", () => on.resolve(it.id));
    btns.append(yes, no);
    card.append(btns);

    root.append(card);
  }
}
