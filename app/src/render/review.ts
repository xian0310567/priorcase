import type { Review } from "../types";
import { vaultLabel } from "../format";
import { renderEmpty } from "./shell";
import { el, mountExcerpt } from "./excerpt";

export interface ReviewActions {
  /** 노트가 사실대로 쓰였다 → `prior reviewed <id>`
   *
   * **승격 ID 를 넘긴다. stem 이 아니다.** 검토 표시는 승격 원장에 남고 그
   * 키가 ID 다.
   *
   * outcome 을 건드리지 않는다 — 그건 "그 결정이 결과적으로 좋았나" 라는 다른
   * 질문이고, 회고 큐가 그 값이 정해진 노트를 영영 제외한다. */
  ok(id: string): void;
  /** 노트를 연다 (고치거나 지우는 것은 사람이 에디터에서 한다).
   *
   * **stem 을 넘긴다.** 앱이 볼트 경로를 조립하지 않으려면 `prior path <stem>`
   * 에게 물어야 하고, 그 명령이 stem 을 받는다. */
  open(stem: string): void;
}

const EXCERPT_LINES = 12;

/** stemOf 는 볼트 상대 경로에서 결정 노트의 stem 을 뽑는다.
 *
 * **`.md` 만 뗀다.** 마지막 점부터 자르면 안 된다 — 볼트 폴더 이름에 점이 있다
 * (실측: `draft.ai/decisions/…`). */
export function stemOf(path: string): string {
  const base = path.split("/").pop() ?? path;
  return base.endsWith(".md") ? base.slice(0, -3) : base;
}

export function renderReview(root: HTMLElement, items: Review[], on: ReviewActions): void {
  root.replaceChildren();
  if (items.length === 0) {
    renderEmpty(root, "검토할 노트");
    return;
  }
  for (const it of items) {
    const stem = stemOf(it.path);
    const hasExcerpt = it.excerpt.trim() !== "";
    const card = el("div", "card");

    const head = el("div", "card-head");
    head.append(
      el("span", "when", it.at.slice(0, 10)),
      el("span", "vault", vaultLabel(it.domain, it.vault)),
    );
    card.append(head);

    // 발췌 | 노트 — 나란히
    const split = el("div", "split");

    const left = el("div", "split-side");
    left.append(el("div", "split-title", "판별기가 본 발췌"));
    if (!hasExcerpt) {
      // **없는 것을 조용히 안 보여 주면 사람은 노트만 보고 맞다고 누른다.**
      // 옛 원장 줄(2026-08-12 이전)에는 발췌가 없다 — 실측으로 지금 검토 큐
      // 3건이 전부 그렇다.
      left.append(
        el("p", "missing", "⚠️ 대조할 발췌가 없다 (옛 기록). 노트를 직접 열어 확인하라."),
      );
    } else {
      mountExcerpt(left, it.excerpt, EXCERPT_LINES);
    }

    const right = el("div", "split-side");
    right.append(el("div", "split-title", "판별기가 쓴 노트"));
    right.append(el("div", "stem", stem), el("div", "path", it.path));

    split.append(left, right);
    card.append(split);

    const btns = el("div", "buttons");
    const ok = el("button", "primary", "맞다") as HTMLButtonElement;
    // **대조할 것이 없으면 "맞다" 를 막는다.**
    //
    // 발췌 없이 "판별기가 사실대로 썼다" 고 표시하는 것은 검증이 아니라 서명이다.
    // 이 화면이 있는 이유가 판별기의 날조를 잡는 것인데, 근거 없이 누르게 두면
    // 그 이유가 사라진다. 노트를 열어 본 뒤에는 사람이 CLI 로 표시할 수 있다.
    ok.disabled = !hasExcerpt;
    if (!hasExcerpt) ok.title = "대조할 발췌가 없다 — 노트를 열어 확인하라";
    ok.addEventListener("click", () => {
      if (!hasExcerpt) return;
      on.ok(it.id);
    });
    const open = el("button", "secondary", "노트 열기") as HTMLButtonElement;
    open.addEventListener("click", () => on.open(stem));
    btns.append(ok, open);
    card.append(btns);

    root.append(card);
  }
}
