import { invoke } from "@tauri-apps/api/core";
import type { Queue, Settings, CmdError } from "./types";
import {
  fetchQueue,
  fetchSettings,
  setHost,
  addVault,
  bindDomain,
  openVault,
} from "./api";
import { renderHosts } from "./render/hosts";
import { renderVaults } from "./render/vaults";
import { renderHealth } from "./render/health";
import { renderError, renderWarnings } from "./render/shell";
import { backlogLine } from "./format";

// 앱은 **설정 콘솔**이다 (2026-08-14 전환).
//
// 예전에는 확인·검토·회고 큐를 사람에게 보여 주고 승인·판정을 받았다. 그런데
// 이 도구의 근간은 자동 기록이다 — 사람이 "이걸 기록할까요" 를 누르는 순간 그
// 전제를 사람이 대신 갚는다. 승격 원장 실측(136건 중 기록 3건)이 그 큐가
// 기능이 아니라 자동 층이 못 따라잡은 잔해임을 보여 줬다.
//
// 대신 앱은 지금 아무 데서도 못 하는 일을 맡는다: 어느 도구의 대화를 훑을지,
// 프로젝트를 어느 볼트에 엮을지, 볼트를 어디에 만들지.

type Tab = "hosts" | "vaults" | "health";

const TABS: Tab[] = ["hosts", "vaults", "health"];
const LABEL: Record<Tab, string> = {
  hosts: "호스트",
  vaults: "볼트",
  health: "상태",
};

/** badgeText 는 메뉴바에 띄울 글자다.
 *
 * **고장났을 때만 말한다.** 예전에는 큐 건수를 띄웠는데, 그건 사람에게 할 일이
 * 있다는 뜻이었다. 지금 앱에는 사람이 눌러야 할 일감이 없다 — 밀린 구간은
 * 데몬이 소화한다.
 *
 * 늘 무언가 떠 있으면 사람이 그것을 무시하는 법을 배우고, 그러면 진짜 고장이
 * 났을 때도 안 보인다. */
export function badgeText(q: Queue): string {
  return q.health.some((h) => h.level === "fail") ? "⚠" : "";
}

/** POLL_MS 는 다시 읽는 주기다.
 *
 * 설정 화면은 **사람이 바꿀 때만 바뀐다.** 예전 큐 화면은 백그라운드에서 큐가
 * 늘어나므로 30초마다 읽었지만, 지금은 그럴 이유가 거의 없다. 그래도 폴링을
 * 남기는 이유는 상태 검사다 — 볼트가 빠지거나 판별기 로그인이 풀리는 것은
 * 앱 밖에서 일어난다. */
const POLL_MS = 60_000;

function start(app: HTMLElement): void {
  let tab: Tab = "hosts";

  function shell(): { tabs: HTMLElement; banner: HTMLElement; body: HTMLElement } {
    app.replaceChildren();
    const tabs = document.createElement("nav");
    tabs.className = "tabs";
    const banner = document.createElement("div");
    const body = document.createElement("div");
    body.className = "body";
    app.append(tabs, banner, body);
    return { tabs, banner, body };
  }

  function draw(q: Queue, s: Settings): void {
    const { tabs, banner, body } = shell();

    for (const t of TABS) {
      const b = document.createElement("button");
      b.className = "tab" + (t === tab ? " active" : "");
      b.textContent = LABEL[t];
      b.addEventListener("click", () => {
        tab = t;
        draw(q, s);
      });
      tabs.append(b);
    }

    // 두 곳의 경고를 합친다 — 사람은 어느 명령이 낸 것인지 모르고 알 필요도 없다.
    renderWarnings(banner, [...(q.warnings ?? []), ...(s.warnings ?? [])]);

    const act = async (fn: () => Promise<void>): Promise<void> => {
      try {
        await fn();
        await refresh();
      } catch (e) {
        // **쓰기가 실패하면 조용히 넘어가지 않는다.** 화면만 바뀌고 설정에
        // 반영이 안 되면 사람은 껐다고 믿는다.
        renderError(body, e as CmdError);
      }
    };

    switch (tab) {
      case "hosts":
        renderHosts(body, s.hosts, {
          toggle: (name, enabled) => void act(() => setHost(name, enabled)),
        });
        break;
      case "vaults":
        renderVaults(body, s, {
          open: (name) => void act(() => openVault(name)),
          add: (name) => void act(() => addVault(name)),
          bind: (prefix, vault) => void act(() => bindDomain(prefix, vault)),
        });
        break;
      case "health":
        renderHealth(body, q.health, backlogLine(q.confirm.length, q.retro.length));
        break;
    }
  }

  async function refresh(): Promise<void> {
    try {
      // **둘을 같이 읽는다.** 하나만 실패해도 화면 전체가 오류다 — 설정이 안
      // 읽히는데 상태만 그리면 사람은 앱이 멀쩡한 줄 안다.
      const [q, s] = await Promise.all([fetchQueue(), fetchSettings()]);
      await invoke("set_tray_title", { title: badgeText(q) });
      draw(q, s);
    } catch (e) {
      const { body } = shell();
      renderError(body, e as CmdError);
    }
  }

  void refresh();
  setInterval(() => void refresh(), POLL_MS);
}

// **최상위에서 바로 돌지 않는다.** 그러면 이 모듈을 import 하는 것만으로 폴링이
// 시작되고, 시험에서 `#app` 이 없어 최상위가 터진다 — 실제로 그랬다.
const root = document.querySelector<HTMLDivElement>("#app");
if (root) start(root);
