import { describe, it, expect, vi, beforeEach } from "vitest";
import type { CmdError } from "../src/types";

// invoke 를 가짜로 바꾼다. 이 파일이 검사하는 것은 **경계의 규율**이지
// Tauri 가 동작하는가가 아니다.
const invoke = vi.hoisted(() => vi.fn());
vi.mock("@tauri-apps/api/core", () => ({ invoke }));

const { fetchQueue, fetchSettings, setHost, addVault, bindDomain, openVault } = await import(
  "../src/api",
);

// ★★ **중괄호를 빼면 안 된다.**
//
// `beforeEach(() => invoke.mockReset())` 는 mockReset() 의 반환값(체이닝용 목
// 자신)을 돌려주고, vitest 는 beforeEach 가 돌려준 **함수를 정리 콜백으로
// 취급해 매 테스트 뒤에 부른다.** 즉 테스트마다 invoke() 가 한 번 더 불리고,
// 거부하도록 설정돼 있으면 그 프라미스를 아무도 안 받아 unhandled rejection 이
// 된다. vitest 는 그것을 "Unknown Error" 로 그 테스트에 붙인다 —
// **단언은 통과했는데 테스트는 실패한다.**
//
// 실측으로 잡았다: 목을 한 번도 안 부르는 테스트조차 같은 이유로 실패했다.
beforeEach(() => {
  invoke.mockReset();
});

describe("fetchQueue", () => {
  it("정상 JSON 을 큐로 준다", async () => {
    invoke.mockResolvedValue(
      JSON.stringify({ confirm: [], review: [], retro: [], health: [] }),
    );
    const q = await fetchQueue();
    expect(q.confirm).toEqual([]);
  });

  // ★★★ **깨진 JSON 을 빈 큐로 그리면 안 된다.**
  //
  // 이 시스템은 모든 부품이 실패해도 대화를 막지 않도록 설계돼 있다. 앱마저
  // 조용히 빈 큐로 넘어가면 **고장이 "할 일 없음" 으로 보이고**, 고장을
  // 알아챌 자리가 아무 데도 남지 않는다. 반드시 던져야 한다.
  it("깨진 JSON 은 던진다 — 빈 큐로 뭉개지 않는다", async () => {
    invoke.mockResolvedValue("이건 JSON 이 아니다");
    await expect(fetchQueue()).rejects.toMatchObject({ kind: "failed" });
  });

  // ★ 던지는 오류에 **원문 조각**이 있어야 한다. 없으면 사람이 무엇이 나왔는지
  //   알 방법이 없다 (앱은 stdout 을 어디에도 안 남긴다).
  it("깨진 출력의 앞부분을 단서로 담는다", async () => {
    invoke.mockResolvedValue("panic: runtime error: index out of range");
    await expect(fetchQueue()).rejects.toMatchObject({
      message: expect.stringContaining("panic: runtime error"),
    });
  });

  it("Rust 가 준 CmdError 는 그대로 통과시킨다", async () => {
    invoke.mockRejectedValue({ kind: "not_found", message: "prior 를 찾을 수 없다" });
    await expect(fetchQueue()).rejects.toMatchObject({ kind: "not_found" });
  });

  // ★★ **invoke 는 CmdError 가 아닌 것도 던진다.**
  //
  // 커맨드 이름이 틀렸거나 IPC 자체가 끊기면 문자열이나 Error 가 온다. 그걸
  // 그대로 화면에 넘기면 `e.kind` 가 undefined 라 **어느 분기에도 안 걸리고
  // 화면이 통째로 빈다** — 가장 알아채기 어려운 실패다.
  it("CmdError 가 아닌 것도 kind 를 붙여 던진다", async () => {
    invoke.mockRejectedValue("Command queue not found");
    const e = await fetchQueue().then(
      () => null,
      (x) => x as CmdError,
    );
    expect(e, "던졌어야 한다").not.toBeNull();
    expect(e!.kind).toBe("io");
    expect(e!.message).toContain("Command queue not found");
  });
});

describe("fetchSettings", () => {
  it("정상 JSON 을 설정으로 준다", async () => {
    invoke.mockResolvedValue(
      JSON.stringify({ config_path: "/c", vaults: [], domains: [], hosts: [] }),
    );
    const s = await fetchSettings();
    expect(s.config_path).toBe("/c");
  });

  // ★★★ **깨진 설정을 빈 설정으로 그리면 안 된다.**
  //
  // 빈 볼트 목록은 "볼트가 없다" 로 읽히고, 사람은 멀쩡한 볼트가 있는데
  // 새로 만들려 든다.
  it("깨진 JSON 은 던진다", async () => {
    invoke.mockResolvedValue("panic: ...");
    await expect(fetchSettings()).rejects.toMatchObject({ kind: "failed" });
  });

  // ★ 어느 명령이 깨졌는지 말해야 한다 — queue 와 settings 를 같이 읽으므로
  //   메시지가 같으면 어디를 봐야 할지 모른다.
  it("어느 명령이 깨졌는지 말한다", async () => {
    invoke.mockResolvedValue("쓰레기");
    await expect(fetchSettings()).rejects.toMatchObject({
      message: expect.stringContaining("settings"),
    });
  });
});

describe("설정을 고치는 명령", () => {
  // ★★ 인자 이름이 Rust 쪽 파라미터명과 어긋나면 **조용히 안 먹는다** —
  //    Tauri 가 빠진 인자를 빈 문자열로 채워 넘기는 판이 있다.
  it("setHost 는 이름과 켜짐을 그대로 넘긴다", async () => {
    invoke.mockResolvedValue(undefined);
    await setHost("Codex CLI", false);
    expect(invoke).toHaveBeenCalledWith("set_host", { name: "Codex CLI", enabled: false });
  });

  // ★★★ **경로를 안 넘긴다.**
  //
  // 어디에 만들지는 답이 이미 정해져 있는 질문이다 — 지금 볼트 옆이다.
  // 그 규칙이 앱에도 살면 CLI 와 어긋날 때 앱이 엉뚱한 자리를 보여 준다.
  it("addVault 는 이름만 넘긴다", async () => {
    invoke.mockResolvedValue(undefined);
    await addVault("회사");
    expect(invoke).toHaveBeenCalledWith("add_vault", { name: "회사" });
  });

  // ★★★ **빈 볼트는 "기본 볼트로 되돌린다" 는 뜻이고 그대로 넘어가야 한다.**
  //
  // 여기서 임의로 기본 이름을 채워 넣으면 설정에 vault 줄이 남고, 나중에 기본
  // 볼트의 이름이 바뀔 때 그 프로젝트만 갈 곳을 잃는다.
  it("bindDomain 은 빈 볼트를 그대로 넘긴다", async () => {
    invoke.mockResolvedValue(undefined);
    await bindDomain("omni", "");
    expect(invoke).toHaveBeenCalledWith("bind_domain", { prefix: "omni", vault: "" });
  });

  it("openVault 는 이름으로 부른다 — 경로가 아니다", async () => {
    invoke.mockResolvedValue(undefined);
    await openVault("default");
    expect(invoke).toHaveBeenCalledWith("open_vault", { name: "default" });
  });

  it("쓰기가 실패하면 던진다 — 성공한 척하지 않는다", async () => {
    invoke.mockRejectedValue({ kind: "timeout", message: "느리다" });
    await expect(setHost("Codex CLI", true)).rejects.toMatchObject({ kind: "timeout" });
  });
});
