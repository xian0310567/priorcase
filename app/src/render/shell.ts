import type { CmdError } from "../types";

/** el 은 요소 하나를 만든다.
 *
 * **텍스트는 언제나 textContent 로 넣는다.** 화면에 오는 글의 상당수가 남의
 * 글이다 — prior 의 stderr, 노트 요약, 발췌. innerHTML 을 쓰면 그 안의 마크업이
 * 화면 구조를 뒤튼다. */
function el(tag: string, cls: string, text?: string): HTMLElement {
  const n = document.createElement(tag);
  n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

/** renderError 는 **오류 종류마다 다른 화면**을 그린다.
 *
 * 한 화면으로 뭉치면 "설치가 안 됐다" 와 "설정이 깨졌다" 와 "느리다" 가 같아
 * 보이고, 사람은 무엇을 고쳐야 할지 모른다.
 *
 * 30초마다 다시 불린다 — replaceChildren 으로 매번 갈아 끼운다. */
export function renderError(root: HTMLElement, e: CmdError): void {
  root.replaceChildren();
  const box = el("div", "error");
  switch (e.kind) {
    case "not_found":
      box.append(
        el("h2", "error-title", "prior 가 설치돼 있지 않다"),
        el(
          "p",
          "error-body",
          "터미널에서 `go build -o prior ./cmd/prior && install -m 0755 ./prior ~/.local/bin/prior` " +
            "를 실행하거나, PRIORCASE_APP_BIN 으로 경로를 지정하라.",
        ),
      );
      break;
    case "timeout":
      box.append(
        el("h2", "error-title", "prior 가 너무 오래 걸린다"),
        el(
          "p",
          "error-body",
          "볼트가 크면 큐 계산이 느려질 수 있다. `prior queue --json` 을 직접 돌려 " +
            "얼마나 걸리는지 재 보라.",
        ),
      );
      break;
    default:
      // failed · io · 그리고 **모르는 kind** 가 여기로 온다.
      //
      // 모르는 값을 조용히 삼키면 화면이 빈 상자가 된다. 계약이 늘어나도 최소한
      // 단서(message)는 나와야 한다 — 그게 사람이 고칠 유일한 실마리다.
      box.append(
        el("h2", "error-title", "prior 가 실패했다"),
        el("pre", "error-stderr", e.message),
      );
  }
  root.append(box);
}

/** renderWarnings 는 큐가 불완전하다는 사실을 위에 띄운다.
 *
 * **비어 있으면 아무것도 안 그린다.** 늘 배너가 있으면 사람이 그것을 무시하는
 * 법을 배우고, 그러면 진짜 경고가 안 보인다.
 *
 * 경고가 해소되면 배너도 사라져야 한다 — 볼트가 잠깐 안 붙었다가 붙는 판이
 * 흔하다(외장 디스크 · 동기화). 안 지우면 사람은 멀쩡한 큐를 보면서 영영
 * "불완전" 을 읽는다. */
export function renderWarnings(root: HTMLElement, warnings: string[] | undefined): void {
  root.replaceChildren();
  if (!warnings || warnings.length === 0) return;
  const box = el("div", "warnings");
  box.append(el("strong", "warnings-title", "⚠️ 큐가 불완전하다"));
  const ul = el("ul", "warnings-list");
  for (const w of warnings) ul.append(el("li", "warnings-item", w));
  box.append(ul);
  root.append(box);
}

/** renderEmpty 는 빈 상태를 **고장이 아닌 것처럼** 그린다. */
export function renderEmpty(root: HTMLElement, what: string): void {
  root.replaceChildren(el("p", "empty", `${what}이 없다.`));
}
