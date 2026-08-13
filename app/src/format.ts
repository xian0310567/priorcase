/** vaultLabel 은 "어느 프로젝트의, 어느 볼트에" 를 한 줄로 만든다.
 *
 * **앱에는 cwd 가 없다.** 메뉴바에 상주하므로 큐가 볼트를 전부 덮는다. 줄에
 * 볼트가 안 붙으면 사람이 "이게 어디에 쓰일 건지" 를 알 수 없다.
 *
 * 볼트가 비면 **설정 오류**다 — 도메인이 설정에 없는 볼트를 가리킨다는 뜻이고,
 * 그 경우 기록 자체가 실패한다(capture 가 거부한다). 기본 볼트로 그리면 사람은
 * 거기 쌓이는 줄 알고, 보여 준 것과 실제가 어긋난다. */
export function vaultLabel(domain: string, vault: string): string {
  return vault ? `${domain} · ${vault} 볼트` : `${domain} · ⚠️ 볼트 미상`;
}

/** clip 은 긴 글을 줄 단위로 접고 **몇 줄이 숨었는지** 함께 준다.
 *
 * 발췌가 실측 880B~4.9KB 이고 절반이 한 화면에 안 들어간다. 숨은 줄 수를 안
 * 보여 주면 사람은 그게 전부인 줄 안다 — 결정이 잘린 뒷부분에 있으면 그대로
 * 놓친다.
 *
 * hidden 은 **실제로 안 보이는 줄 수**여야 한다. 어림수를 내면 사람은 "3줄 더"
 * 를 믿고 펼쳤다가 40줄을 만난다. */
export function clip(text: string, lines: number): { shown: string; hidden: number } {
  const all = text.split("\n");
  if (all.length <= lines) return { shown: text, hidden: 0 };
  return { shown: all.slice(0, lines).join("\n"), hidden: all.length - lines };
}

/** reasonLabel 은 회고 방아쇠를 사람 말로 바꾼다.
 *
 * superseded 는 hits 가 0 일 수 있다 — 뒤집혔다는 사실만으로 올라오기 때문이다.
 * 그때 "재회수 0회" 로 그리면 거짓이다.
 *
 * **모르는 값은 뭉개지 않고 그대로 보인다.** 지금 Go 쪽 Reason 은 둘뿐이지만
 * 늘어날 수 있고, TS 타입은 런타임 JSON 을 검사하지 않는다. else 로 "재회수
 * N회" 를 내면 새 방아쇠가 전부 거짓말로 그려지고 아무도 눈치채지 못한다.
 * 낯선 문자열이 화면에 뜨면 시끄럽다 — 그게 낫다. */
export function reasonLabel(reason: string, hits: number): string {
  if (reason === "superseded") return "뒤집혔다";
  if (reason === "recalled") return `재회수 ${hits}회`;
  return reason;
}
