// prior queue --json 의 계약이다.
//
// **손으로 쓴다. 생성하지 않는다.** 계약이 바뀌면 여기가 컴파일 에러로 깨져야
// 하고, 그 순간이 "앱도 같이 고쳐야 한다" 를 알아채는 자리다. Go 쪽의
// queue_test.go 가 같은 목록을 못 박는다.
//
// 아래 주석의 "실측" 은 2026-08-13 `prior queue --json` 출력(확인 23 · 검토 3 ·
// 회고 49 · 상태 10)과 `internal/adapter/cli/queue.go` 의 구조체를 대조한 것이다.

export type Level = "ok" | "warn" | "fail" | "unknown";

export interface Similar {
  stem: string;
  /** 볼트 상대 경로. 여는 데 쓰려면 `prior path <stem>` 이 필요하다. */
  path: string;
  summary: string;
  /** 회수 점수. **절대값으로 판정하면 안 된다** — 진짜 일치 65점과 가짜 1위
   * 54점이 겹친 실측이 있다. 같은 목록 안의 상대 비교로만 쓸모가 있다. */
  score: number;
}

export interface Confirm {
  id: string;
  domain: string;
  /** 빈 문자열이면 "볼트를 모른다"(설정 오류)다. 기본 볼트로 그리면 안 된다. */
  vault: string;
  when: string;
  /** 항상 배열이다 — Go 쪽이 nil 을 []로 바꿔서 낸다. 판별기가 있으면 시그널
   * 필터를 건너뛰므로 **비는 것이 정상이고 흔하다** (실측 27건 중 2건). */
  signals: string[];
  excerpt: string;
  fails: number;
  /** true 면 자동으로는 다시 안 온다 — 기다리라고 그리면 안 된다. */
  gave_up: boolean;
  /** 항상 배열이다 (Go 가 [] 로 초기화한다). 비슷한 것이 없거나 발췌가 빈
   * 옛 구간이면 빈 배열이다. */
  similar: Similar[];
}

export interface Review {
  id: string;
  domain: string;
  vault: string;
  at: string;
  path: string;
  /** 빈 문자열일 수 있다 (2026-08-12 이전 원장). 그때는 "대조할 발췌가 없다"
   * 고 말해야 한다 — 조용히 안 보여 주면 사람은 노트만 보고 맞다고 누른다. */
  excerpt: string;
}

export interface Retro {
  stem: string;
  date: string;
  domain: string;
  vault: string;
  summary: string;
  /** Go 쪽이 omitempty 다 — 실측 49건 전부 없었다. 있을 때만 그린다. */
  author?: string;
  reason: "recalled" | "superseded";
  /** superseded 만으로 올라온 것은 0 일 수 있다. */
  hits: number;
}

export interface Health {
  name: string;
  level: Level;
  detail: string;
  /** Go 쪽이 omitempty 다. 없으면 고칠 방법을 모른다는 뜻이지 정상이 아니다. */
  fix?: string;
}

export interface Queue {
  confirm: Confirm[];
  review: Review[];
  retro: Retro[];
  health: Health[];
  /** 없으면 키 자체가 빠진다. 있으면 **큐가 불완전하다**는 뜻이다. */
  warnings?: string[];
}

/** IPC 오류. kind 로 화면을 가른다.
 *
 * 모르는 kind 가 오면 "그 밖"(io 처럼) 으로 그려야 한다 — 새 값이 생겼을 때
 * 화면이 통째로 비는 것보다 낫다. */
export interface CmdError {
  kind: "not_found" | "failed" | "timeout" | "io";
  message: string;
}
