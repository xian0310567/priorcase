import type { HostInfo, VaultInfo } from "./types";

/** num 은 큰 수에 자릿점을 찍는다. 기록 1729개는 한눈에 안 읽힌다. */
export function num(n: number): string {
  return n.toLocaleString("en-US");
}

/** hostState 는 호스트 한 줄의 오른쪽 글이다.
 *
 * **"자리 없음" 과 "기록 0개" 는 다른 말이다.** 앞엣것은 그 도구를 안 쓰거나
 * 기록 자리를 옮긴 것이고, 뒤엣것은 도구는 있는데 대화가 없는 것이다. 뭉치면
 * 사람이 무엇을 고쳐야 할지 모른다. */
export function hostState(h: HostInfo): string {
  if (!h.exists) return "기록 자리가 없다";
  return `대화 ${num(h.files)}개`;
}

/** vaultState 는 볼트 한 줄의 오른쪽 글이다.
 *
 * 자리가 없으면 **그것부터 말한다.** 결정 0건으로 그리면 "아직 안 썼구나" 로
 * 읽히는데, 실제로는 그 볼트로 엮인 도메인의 기록이 통째로 안 써지는 상태다. */
export function vaultState(v: VaultInfo): string {
  if (!v.exists) return "⚠️ 자리가 없다";
  // **목록이 아닌 것을 0으로 읽는다.** 낡은 판은 빈 목록을 `null` 로 낸다
  // (Go 의 nil 슬라이스). 2026-09-01 에 볼트를 하나 만들었더니 여기가
  // TypeError 를 냈고, 그 예외가 렌더를 끊어 **볼트 화면이 통째로 사라졌다.**
  const d = Array.isArray(v.domains) ? v.domains.length : 0;
  return `결정 ${num(v.decisions)}건 · 프로젝트 ${d}개`;
}

/** vaultOfDomain 은 도메인이 실제로 쓰는 볼트 이름이다.
 *
 * 빈 값은 "기본 볼트" 라는 뜻이다. 그것을 빈칸으로 그리면 사람은 **엮이지
 * 않았다**고 읽는데, 실제로는 기본 볼트로 잘 가고 있다. */
export function vaultOfDomain(vault: string, fallback: string): string {
  return vault === "" ? fallback : vault;
}

/** backlogLine 은 밀린 구간을 진단 한 줄로 만든다. 없으면 빈 문자열이다.
 *
 * **할 일 목록이 아니다.** 사람이 누를 것이 없다 — 밀린 구간은 데몬이 세션
 * 끝마다 소화한다. 여기 적는 이유는 그 처리량이 새 구간이 쌓이는 속도를 못
 * 따라가면 사람이 그 사실을 알 자리가 아무 데도 없기 때문이다. */
export function backlogLine(pending: number, retro: number): string {
  const parts: string[] = [];
  if (pending > 0) parts.push(`판정을 기다리는 구간 ${num(pending)}건`);
  if (retro > 0) parts.push(`결과를 안 물어본 결정 ${num(retro)}건`);
  return parts.join(" · ");
}
