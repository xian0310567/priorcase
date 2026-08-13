import { invoke } from "@tauri-apps/api/core";
import type { Queue, CmdError } from "./types";

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

export async function fetchQueue(): Promise<Queue> {
  let raw: string;
  try {
    raw = await invoke<string>("queue");
  } catch (e) {
    throw asCmdError(e);
  }
  try {
    return JSON.parse(raw) as Queue;
  } catch {
    // **깨진 JSON 을 빈 큐로 그리면 안 된다.** 그러면 고장이 "할 일 없음" 이 된다.
    //
    // 이 시스템은 모든 부품이 실패해도 대화를 막지 않도록 설계돼 있다. 앱마저
    // 조용히 넘어가면 고장을 알아챌 자리가 아무 데도 남지 않는다.
    throw {
      kind: "failed",
      message: `prior 가 JSON 이 아닌 것을 냈다: ${raw.slice(0, 200)}`,
    } as CmdError;
  }
}

export async function resolvePending(id: string): Promise<void> {
  try {
    await invoke("resolve_pending", { id });
  } catch (e) {
    throw asCmdError(e);
  }
}

export async function promote(id: string): Promise<void> {
  try {
    await invoke<string>("promote", { id });
  } catch (e) {
    throw asCmdError(e);
  }
}

/** markReviewed 는 "판별기가 사실대로 썼다" 는 검증 표시다.
 *
 * **승격 ID 를 넘긴다. stem 이 아니다.** 표시는 승격 원장에 남고 그 키가 ID 다.
 *
 * review(outcome) 과 다른 명령인 이유: outcome 은 "그 결정이 결과적으로 좋았나"
 * 이고 회고 큐가 그 값이 정해진 노트를 영영 제외한다. 둘을 한 값에 실으면 노트를
 * 검증했을 뿐인데 나중에 결과를 묻는 자리가 조용히 사라진다. */
export async function markReviewed(id: string): Promise<void> {
  try {
    await invoke("mark_reviewed", { id });
  } catch (e) {
    throw asCmdError(e);
  }
}

/** openNote 는 결정 노트를 OS 기본 앱으로 연다.
 *
 * 경로 해석과 실행은 Rust 쪽이 한다 — 앱이 볼트 경로를 조립하면 볼트 선택
 * 규칙이 둘이 된다. */
export async function openNote(stem: string): Promise<void> {
  try {
    await invoke("open_note", { stem });
  } catch (e) {
    throw asCmdError(e);
  }
}

export async function review(stem: string, outcome: "good" | "bad"): Promise<void> {
  try {
    await invoke("review", { stem, outcome });
  } catch (e) {
    throw asCmdError(e);
  }
}
