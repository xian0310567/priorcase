import { invoke } from "@tauri-apps/api/core";
import type { Queue, Settings, CmdError } from "./types";

/** **여기만 백엔드를 안다.** 화면 코드가 invoke 를 직접 부르면 가짜로 바꿔
 * 테스트하기가 어려워지고, 커맨드 이름이 화면마다 흩어진다. */

/** asCmdError 는 무엇이 넘어오든 CmdError 로 만든다.
 *
 * Rust 쪽은 {kind, message} 를 주지만, invoke 는 **그 전에도 던진다** —
 * 커맨드 이름이 틀렸거나 IPC 자체가 끊기면 문자열이나 Error 가 온다.
 * 그때 `e.kind` 를 읽으면 undefined 가 되고 화면이 통째로 빈다. */
function asCmdError(e: unknown): CmdError {
  if (e && typeof e === "object" && "kind" in e && "message" in e) {
    return e as CmdError;
  }
  return { kind: "io", message: String(e) };
}

/** parseOr 는 문자열을 JSON 으로 읽는다.
 *
 * **깨진 JSON 을 빈 값으로 그리면 안 된다.** 그러면 고장이 "할 일 없음" 이 된다.
 * 이 시스템은 모든 부품이 실패해도 대화를 막지 않도록 설계돼 있다. 앱마저
 * 조용히 넘어가면 고장을 알아챌 자리가 아무 데도 남지 않는다. */
function parseOr<T>(raw: string, what: string): T {
  try {
    return JSON.parse(raw) as T;
  } catch {
    throw {
      kind: "failed",
      message: `prior 가 ${what}에서 JSON 이 아닌 것을 냈다: ${raw.slice(0, 200)}`,
    } as CmdError;
  }
}

async function call<T>(cmd: string, args?: Record<string, unknown>): Promise<T> {
  try {
    return (await invoke(cmd, args)) as T;
  } catch (e) {
    throw asCmdError(e);
  }
}

export async function fetchQueue(): Promise<Queue> {
  return parseOr<Queue>(await call<string>("queue"), "queue");
}

export async function fetchSettings(): Promise<Settings> {
  return parseOr<Settings>(await call<string>("settings"), "settings");
}

/** setHost 는 그 호스트의 대화 기록을 훑을지 정한다.
 *
 * 이름은 **레지스트리의 이름 그대로** 넘긴다 ("Codex CLI"). CLI 가 모르는
 * 이름을 거부하므로 오타는 화면에 오류로 뜬다 — 조용히 안 먹지 않는다. */
export async function setHost(name: string, enabled: boolean): Promise<void> {
  await call<void>("set_host", { name, enabled });
}

export async function addVault(name: string, path: string): Promise<void> {
  await call<void>("add_vault", { name, path });
}

/** bindDomain 은 프로젝트가 쓸 볼트를 정한다.
 *
 * vault 가 빈 문자열이면 **기본 볼트로 되돌린다** — Rust 쪽이 그때 인자를
 * 빼고 부른다. */
export async function bindDomain(prefix: string, vault: string): Promise<void> {
  await call<void>("bind_domain", { prefix, vault });
}

/** openVault 는 볼트 폴더를 OS 파일 관리자로 연다.
 *
 * **이름으로 부른다. 경로가 아니다.** 경로 해석은 Rust 가 `prior settings` 에
 * 물어본다 — 프런트가 임의의 경로를 열 수 있게 두면 그 창구가 앱의 다른 어떤
 * 기능보다 넓어진다. */
export async function openVault(name: string): Promise<void> {
  await call<void>("open_vault", { name });
}
