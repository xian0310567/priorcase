import { invoke } from "@tauri-apps/api/core";
import {
  fetchQueue,
  resolvePending,
  promote,
  review,
  markReviewed,
  openNote,
} from "./api";
import type { Queue, CmdError } from "./types";
import { renderConfirm } from "./render/confirm";
import { renderReview } from "./render/review";
import { renderRetro } from "./render/retro";
import { renderHealth } from "./render/health";
import { renderError, renderWarnings } from "./render/shell";

type Tab = "confirm" | "review" | "retro" | "health";

const TABS: Tab[] = ["confirm", "review", "retro", "health"];
const LABEL: Record<Tab, string> = {
  confirm: "확인",
  review: "검토",
  retro: "회고",
  health: "상태",
};

/** badgeText 는 메뉴바에 붙일 글자다.
 *
 * **할 일이 없으면 빈 문자열이다.** 늘 무언가 떠 있으면 사람이 그것을 무시하는
 * 법을 배우고, 그러면 진짜 할 일이 있을 때도 안 보인다.
 *
 * **합계 하나로 낸다.** 메뉴바는 폭이 좁다 — 종류별로 나열하면 실측 숫자로
 * "확인 23 · 검토 3 · 회고 50" 이 되어 다른 아이콘을 밀어낸다. 어느 큐가
 * 줄었는지는 앱을 열면 탭마다 보인다.
 *
 * **fail 만 ⚠ 를 띄운다.** warn 은 실측 10건 중 2건이 상시라(팀 이식성·색인)
 * 경고 표시가 영구히 켜진다. 모르는 등급도 fail 로 읽지 않는다 — 등급이 하나
 * 늘 때마다 배지가 경고로 굳는다. */
export function badgeText(q: Queue): string {
  const n = q.confirm.length + q.review.length + q.retro.length;
  const broken = q.health.some((h) => h.level === "fail");
  if (broken) return n > 0 ? `⚠${n}` : "⚠";
  return n > 0 ? String(n) : "";
}

/** POLL_MS 는 폴링 주기다.
 *
 * 파일 감시(fsnotify)를 안 쓰는 이유: 볼트 여럿과 상태 디렉토리를 다 봐야 하고,
 * 그래도 회고 큐는 재계산해야 한다. 얻는 것이 적다. */
const POLL_MS = 30_000;

/** start 는 앱을 띄운다.
 *
 * **최상위에서 바로 돌지 않는다.** 그러면 이 모듈을 import 하는 것만으로 폴링이
 * 시작되고, 시험에서 `#app` 이 없어 최상위가 터진다 — 실제로 그랬다.
 * 부트스트랩을 함수 안에 두면 badgeText 만 꺼내 보는 것이 안전해진다. */
function start(app: HTMLElement): void {
  let tab: Tab = "confirm";

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

  function draw(q: Queue): void {
    const { tabs, banner, body } = shell();

    for (const t of TABS) {
      const n = t === "health" ? 0 : q[t].length;
      const b = document.createElement("button");
      b.className = "tab" + (t === tab ? " active" : "");
      b.textContent = n > 0 ? `${LABEL[t]} ${n}` : LABEL[t];
      b.addEventListener("click", () => {
        tab = t;
        draw(q);
      });
      tabs.append(b);
    }

    renderWarnings(banner, q.warnings);

    const act = async (fn: () => Promise<void>): Promise<void> => {
      try {
        await fn();
        await refresh();
      } catch (e) {
        // **쓰기가 실패하면 조용히 넘어가지 않는다.** 큐에서 사라졌는데 볼트에
        // 반영이 안 되면 사람은 답한 줄 안다.
        renderError(body, e as CmdError);
      }
    };

    switch (tab) {
      case "confirm":
        renderConfirm(body, q.confirm, {
          resolve: (id) => void act(() => resolvePending(id)),
          promote: (id) => void act(() => promote(id)),
        });
        break;
      case "review":
        renderReview(body, q.review, {
          // 승격 ID 로 검토 표시만 남긴다 — outcome 은 회고가 나중에 물을
          // 다른 질문이다 (§ priorcase-결정-검토표시를-원장에-outcome과분리).
          ok: (id) => void act(() => markReviewed(id)),
          open: (stem) => void act(() => openNote(stem)),
        });
        break;
      case "retro":
        renderRetro(body, q.retro, {
          judge: (stem, outcome) => void act(() => review(stem, outcome)),
          open: (stem) => void act(() => openNote(stem)),
        });
        break;
      case "health":
        renderHealth(body, q.health);
        break;
    }
  }

  async function refresh(): Promise<void> {
    try {
      const q = await fetchQueue();
      await invoke("set_tray_title", { title: badgeText(q) });
      draw(q);
    } catch (e) {
      // **빈 큐로 그리지 않는다.** 고장이 "할 일 없음" 이 되면 앱도 이 시스템의
      // 병(고장이 정상과 구별되지 않음)에 걸린다.
      const { banner, body } = shell();
      renderWarnings(banner, undefined);
      renderError(body, e as CmdError);
      await invoke("set_tray_title", { title: "⚠" });
    }
  }

  void refresh();
  setInterval(() => void refresh(), POLL_MS);
  // 창을 열 때도 갱신한다 — 30초를 기다리게 하면 오래된 것을 보게 된다.
  window.addEventListener("focus", () => void refresh());
}

const root = document.querySelector<HTMLDivElement>("#app");
if (root) start(root);
