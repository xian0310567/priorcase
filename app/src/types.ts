// prior 의 JSON 출력 계약이다.
//
// **손으로 쓴다. 생성하지 않는다.** 계약이 바뀌면 여기가 컴파일 에러로 깨져야
// 하고, 그 순간이 "앱도 같이 고쳐야 한다" 를 알아채는 자리다. Go 쪽의
// queue_test.go · settings_test.go 가 같은 목록을 못 박는다.

export type Level = "ok" | "warn" | "fail" | "unknown";

export interface Health {
  name: string;
  level: Level;
  detail: string;
  /** Go 쪽이 omitempty 다. 없으면 고칠 방법을 모른다는 뜻이지 정상이 아니다. */
  fix?: string;
}

/** Queue 는 `prior queue --json` 이다.
 *
 * **앱은 이제 health 와 개수만 쓴다.** 확인·검토·회고 큐 화면은 2026-08-14 에
 * 들어냈다 — 사람에게 "이걸 기록할까요" 를 물으면 자동 기록이라는 전제를 사람이
 * 대신 갚는다.
 *
 * 세 배열을 `unknown[]` 로 두는 이유: 개수 말고는 안 쓰는데 필드를 다 적어 두면
 * **안 쓰는 계약을 지키느라 Go 쪽을 못 고치게 된다.** 개수는 진단에 쓴다 —
 * 밀린 구간이 쌓이는 것은 데몬의 처리량 문제이지 사람이 누를 일이 아니다. */
export interface Queue {
  confirm: unknown[];
  review: unknown[];
  retro: unknown[];
  health: Health[];
  /** 없으면 키 자체가 빠진다. 있으면 **큐가 불완전하다**는 뜻이다. */
  warnings?: string[];
}

export interface VaultInfo {
  name: string;
  path: string;
  /** 거짓이면 그 볼트로 엮인 도메인의 기록이 통째로 안 써진다. */
  exists: boolean;
  decisions: number;
  /** 이 볼트를 쓰는 도메인 접두어들. 항상 배열이지만 빌 수 있다. */
  domains: string[];
}

export interface DomainInfo {
  prefix: string;
  folder: string;
  /** 빈 문자열이면 기본 볼트를 쓴다는 뜻이다. */
  vault: string;
  paths: string[];
  repos: string[];
}

export interface HostInfo {
  name: string;
  enabled: boolean;
  root: string;
  /** 거짓이면 그 자리가 없다. files 가 0 인 것과 다르다 — 후자는 정말 빈 것이다. */
  exists: boolean;
  files: number;
}

/** Settings 는 `prior settings --json` 이다. */
export interface Settings {
  config_path: string;
  vaults: VaultInfo[];
  domains: DomainInfo[];
  hosts: HostInfo[];
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
