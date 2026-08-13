import { clip } from "../format";

/** el 은 요소 하나를 만든다.
 *
 * **텍스트는 언제나 textContent 로 넣는다.** 화면에 오는 글의 상당수가 남의
 * 글이다 — prior 의 stderr, 대화 원문, 노트 요약, 파일 경로. innerHTML 을 쓰면
 * 그 안의 마크업이 화면 구조를 뒤튼다. */
export function el(tag: string, cls: string, text?: string): HTMLElement {
  const n = document.createElement(tag);
  n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

/** mountExcerpt 는 접힌 발췌와 펼치기 자리를 카드에 붙인다.
 *
 * **확인 큐와 검토 큐가 같은 것을 쓴다.** 한쪽만 고치면 두 화면이 다르게
 * 접히고, 사람은 그 차이를 규칙으로 착각한다.
 *
 * 왜 접는가: 실측(2026-08-13, 확인 23건)에서 발췌가 29~113줄이고 **23건 전부**
 * 접기에 걸린다. 전부 펼쳐 두면 23장이 한 화면에 쏟아져 훑을 수가 없다.
 *
 * 왜 펼칠 수 있어야 하는가: 접힌 채로만 두면 매 건마다 내용의 대부분이 영영 안
 * 보인다. 그 상태로 판단하는 것은 확인이 아니라 판별기를 믿는 것이다.
 *
 * **펼침 상태는 카드마다 따로다.** 하나 눌렀는데 전부 펼쳐지면 훑기가 불가능해진다.
 */
export function mountExcerpt(card: HTMLElement, text: string, lines: number): void {
  const ex = el("pre", "excerpt");
  const more = el("div", "excerpt-more");
  let open = false;
  const paint = (): void => {
    const { shown, hidden } = clip(text, lines);
    ex.textContent = open ? text : shown;
    if (hidden === 0) {
      more.remove();
      return;
    }
    more.textContent = open ? "접는다" : `… ${hidden}줄 더 (눌러서 펼친다)`;
    if (!more.isConnected) card.append(more);
  };
  more.addEventListener("click", () => {
    open = !open;
    paint();
  });
  card.append(ex);
  paint();
}
