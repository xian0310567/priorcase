import { invoke } from "@tauri-apps/api/core";
import type { Queue, Settings, CmdError } from "./types";
import {
  fetchQueue,
  fetchSettings,
  setHost,
  addVault,
  bindDomain,
  openVault,
  setVaultRemote,
  listNotes,
  showNote,
  searchNotes,
  saveBody,
  reviewNote,
} from "./api";
import { renderHosts } from "./render/hosts";
import { renderVaults } from "./render/vaults";
import { renderHealth } from "./render/health";
import { renderBrowse, type BrowseState } from "./render/browse";
import type { ReviewPatch } from "./render/properties";
import { spliceLines } from "./render/markdown";
import { el, renderError, renderWarnings } from "./render/shell";
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

/** FIRST_TICK_MS 는 첫 상태 검사를 미루는 시간이다.
 *
 * 목록이 먼저 서야 한다 — 그것이 이 앱의 주역이고, 트레이 배지는 몇 초 늦어도
 * 아무것도 안 잃는다. */
const FIRST_TICK_MS = 3_000;

function start(app: HTMLElement): void {
  // **주역은 볼트다.** 설정은 톱니바퀴 뒤로 갔다(2026-09-01 결정) — 처음 한 번
  // 만지고 안 보는 것이라 상시 노출될 이유가 없다.
  let view: "browse" | "settings" = "browse";
  let tab: Tab = "hosts";
  let browse: BrowseState = {
    notes: [], domain: null, selected: null, open: null,
    query: "", results: null, editingBlock: null, draft: "", saving: false, err: null,
  };
  // 마지막으로 읽은 큐. 폴링마다 "읽는 중" 으로 되돌아가지 않게 들고 있는다.
  let lastQ: Queue | null = null;
  let queueErr: CmdError | null = null;

  /** shell 은 **오류만** 그리는 맨 화면이다.
   *
   * 예전에는 탭 줄까지 만들었는데, 볼트 화면에서 난 오류가 설정 탭을 달고
   * 나왔다 — 어디서 난 고장인지가 흐려진다. 탭은 설정의 것이므로 그쪽으로 옮겼다. */
  function shell(): { banner: HTMLElement; body: HTMLElement } {
    app.replaceChildren();
    const banner = document.createElement("div");
    const body = document.createElement("div");
    body.className = "body";
    app.append(banner, body);
    return { banner, body };
  }

  /** settingsShell 은 설정 화면의 **두 칸**이다 — 볼트 화면과 같은 골격.
   *
   * # 왜 탭에서 사이드바로 옮겼나
   *
   * 2026-09-01 지적: 탭 줄에 `볼트` 가 두 번 나왔다. 하나는 볼트 화면으로 돌아가는
   * 문이고 하나는 볼트 설정 탭인데, **같은 낱말이 두 가지를 가리켰다.** 그리고
   * 앱의 주역은 이미 사이드바 + 본문 두 칸인데 설정만 옛 탭 구조라 다른 앱처럼
   * 보였다.
   *
   * 나가는 문은 사이드바 맨 위에 두고 **이름을 안 겹치게** 한다 — 돌아갈 곳의
   * 이름이 아니라 동작으로 부른다. */
  function settingsShell(): { nav: HTMLElement; banner: HTMLElement; body: HTMLElement } {
    app.replaceChildren();
    const wrap = el("div", "pane2 settings-shell");

    const side = el("aside", "side");
    const back = el("button", "side-back", "‹  돌아가기");
    back.addEventListener("click", () => {
      view = "browse";
      drawBrowse();
    });
    const nav = el("nav", "side-nav");
    side.append(back, nav);

    const main = el("main", "content");
    const pane = el("section", "settings");
    const banner = document.createElement("div");
    const body = el("div", "body");
    pane.append(banner, body);
    main.append(pane);

    wrap.append(side, main);
    app.append(wrap);
    return { nav, banner, body };
  }

  /** drawBrowse 는 볼트 화면이다 — 이 앱의 주역.
   *
   * **설정과 달리 큐도 설정도 안 기다린다.** 목록은 `prior list` 하나로 서고,
   * 그것이 0.3초다. 설정 화면의 느린 것들에 묶으면 주역이 곁다리 속도로 뜬다. */
  function drawBrowse(): void {
    // **렌더가 터져도 검은 화면이 되면 안 된다.**
    //
    // 2026-09-01: 속성 패널이 없는 키를 읽어 TypeError 를 냈는데, 그 예외가
    // `renderBrowse` 를 끊어 방금 비운 `wrap` 이 그대로 남았다 — 사람에게는
    // 앱이 통째로 죽은 것으로 보였고 원인을 알 길이 없었다.
    //
    // 개별 방어(properties.ts 의 arr·str)와 **별개로** 이 그물이 필요하다.
    // 다음에 어디서 터질지는 모르지만, 터졌을 때 무엇이 터졌는지는 보여야 한다.
    try {
      drawBrowseInner();
    } catch (e) {
      app.replaceChildren();
      const box = document.createElement("div");
      box.className = "browse-crash";
      app.append(box);
      renderError(box, {
        kind: "render",
        message: `화면을 그리다 멈췄다: ${e instanceof Error ? e.message : String(e)}`,
      });
    }
  }

  function drawBrowseInner(): void {
    // **껍데기를 다시 만들지 않는다.** 매번 새로 만들면 `renderBrowse` 가 직전
    // 스크롤을 읽을 자리가 사라져 고칠 때마다 화면이 맨 위로 튄다 (그 함수의 §).
    // 설정 화면에서 돌아왔거나 오류 경계가 화면을 비웠으면 없으므로 새로 만든다.
    let wrap = app.querySelector<HTMLElement>(":scope > .browse");
    if (!wrap) {
      app.replaceChildren();
      wrap = document.createElement("div");
      wrap.className = "pane2 browse";
      app.append(wrap);
    }
    renderBrowse(wrap, browse, {
      // **도메인을 고르면 열린 결정을 닫는다.** 목록으로 돌아가는 동작이기도
      // 하다 — 브레드크럼의 뒤로가기가 이것을 부른다.
      pickDomain: (d) => {
        browse = { ...browse, domain: d, selected: null, open: null, editingBlock: null };
        drawBrowse();
      },
      pickNote: (stem) => void openNote(stem),
      search: (q) => void runSearch(q),
      // 블록을 누르면 그 자리가 원문이 된다. **모드 전환이 아니다.**
      editBlock: (from, to, text) => {
        browse = { ...browse, editingBlock: { from, to }, draft: text };
        drawBrowse();
      },
      cancelEdit: () => {
        browse = { ...browse, editingBlock: null, draft: "" };
        drawBrowse();
      },
      // **다시 그리지 않는다.** 타자마다 DOM 을 갈면 커서가 튄다 — textarea 는
      // 자기 값을 스스로 들고 있으므로 초안만 기억하면 된다.
      changeDraft: (v) => {
        browse.draft = v;
      },
      commitBlock: () => void commitBlock(),
      review: (patch) => void reviewProps(patch),
      openSettings: () => {
        view = "settings";
        void refresh();
      },
    });
  }

  async function openNote(stem: string): Promise<void> {
    browse = { ...browse, selected: stem, editingBlock: null, draft: "" };
    drawBrowse();
    try {
      const full = await showNote(stem);
      browse = { ...browse, open: full };
    } catch (e) {
      browse = { ...browse, open: null, err: e as CmdError };
    }
    drawBrowse();
  }

  async function runSearch(q: string): Promise<void> {
    browse = { ...browse, query: q };
    if (!q) {
      browse = { ...browse, results: null };
      drawBrowse();
      return;
    }
    try {
      browse = { ...browse, results: await searchNotes(q) };
    } catch (e) {
      browse = { ...browse, results: [], err: e as CmdError };
    }
    drawBrowse();
  }

  /** commitBlock 은 펼친 블록을 원문의 그 줄 범위에만 갈아 끼우고 저장한다.
   *
   * **안 바뀌었으면 아무것도 안 한다.** 블록을 눌렀다가 그냥 다른 데를 누르는 일이
   * 잦은데, 그때마다 파일을 다시 쓰면 볼트의 git 이력이 뜻 없는 커밋으로 찬다. */
  async function commitBlock(): Promise<void> {
    const open = browse.open;
    const at = browse.editingBlock;
    if (!open || !at) return;

    const src = open.body ?? "";
    const before = src.split("\n").slice(at.from, at.to).join("\n");
    if (browse.draft === before) {
      browse = { ...browse, editingBlock: null, draft: "" };
      drawBrowse();
      return;
    }
    const next = spliceLines(src, at.from, at.to, browse.draft);

    browse = { ...browse, saving: true, editingBlock: null, draft: "" };
    drawBrowse();
    try {
      await saveBody(open.stem, next);
      // **저장 뒤에는 다시 읽는다.** CLI 가 앞뒤 빈 줄을 다듬으므로 화면과
      // 파일이 미세하게 달라질 수 있는데, 그 차이를 안 보이면 다음 저장이
      // 사람이 안 쓴 것을 덮어쓴다.
      const full = await showNote(open.stem);
      browse = { ...browse, open: full, saving: false };
      await loadNotes();
    } catch (e) {
      browse = { ...browse, saving: false };
      const { body } = shell();
      renderError(body, e as CmdError);
      return;
    }
    drawBrowse();
  }

  /** loadNotes 는 목록을 읽는다.
   *
   * **실패를 console 로 삼키지 않는다.** 예전에는 그랬고, 그래서 목록이 0건인
   * 화면과 명령이 죽은 화면이 똑같이 보였다 — 이 프로젝트가 죄목으로 드는
   * "조용한 무동작" 그대로다. */
  /** reviewProps 는 속성을 고치고 다시 읽는다.
   *
   * **다시 읽는 것이 중요하다** — `prior review` 는 옛 요약을 summary_history 로
   * 옮기고 태그에서 decision 표식을 지키는 등 화면이 모르는 일을 한다. 보낸 값을
   * 그대로 믿고 그리면 파일과 화면이 갈린다. */
  async function reviewProps(patch: ReviewPatch): Promise<void> {
    const open = browse.open;
    if (!open) return;
    browse = { ...browse, saving: true };
    drawBrowse();
    try {
      await reviewNote(open.stem, patch);
      browse = { ...browse, open: await showNote(open.stem), saving: false };
      await loadNotes();
    } catch (e) {
      browse = { ...browse, saving: false, err: e as CmdError };
    }
    drawBrowse();
  }

  async function loadNotes(): Promise<void> {
    try {
      browse = { ...browse, notes: await listNotes(), err: null };
    } catch (e) {
      browse = { ...browse, err: e as CmdError };
    }
  }

  /** draw 는 설정 화면이다.
   *
   * **렌더가 터져도 빈 화면이 되면 안 된다.** 볼트 화면은 2026-09-01 검은 화면
   * 사고 뒤에 그물을 쳤는데(drawBrowse) 여기는 안 쳤다. 그래서 같은 날 볼트를
   * 하나 만들었을 때 — 새 볼트의 `domains` 가 null 로 와서 TypeError 가 났고 —
   * 그 예외가 렌더를 끊어 **이미 그려진 부분만 남고 뒤가 통째로 사라졌다.**
   * 사람에게는 "볼트를 만들었더니 화면이 없어졌다" 로 보였고, 새로고침해도
   * 같은 자리에서 또 터지니 영구적이었다.
   *
   * 개별 방어(vaults.ts 의 arr)와 **별개로** 이 그물이 필요하다. 다음에 어디서
   * 터질지는 모르지만, 터졌을 때 무엇이 터졌는지는 보여야 한다. */
  function draw(q: Queue | null, s: Settings): void {
    try {
      drawInner(q, s);
    } catch (e) {
      const { body } = shell();
      renderError(body, {
        kind: "render",
        message: `설정을 그리다 멈췄다: ${e instanceof Error ? e.message : String(e)}`,
      });
    }
  }

  function drawInner(q: Queue | null, s: Settings): void {
    const { nav, banner, body } = settingsShell();

    for (const t of TABS) {
      const b = el("button", `side-item${t === tab ? " active" : ""}`);
      b.append(el("span", "side-item-name", LABEL[t]));
      b.addEventListener("click", () => {
        tab = t;
        draw(q, s);
      });
      nav.append(b);
    }

    // 두 곳의 경고를 합친다 — 사람은 어느 명령이 낸 것인지 모르고 알 필요도 없다.
    renderWarnings(banner, [...(q?.warnings ?? []), ...(s.warnings ?? [])]);

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
          remote: (name, url) => void act(() => setVaultRemote(name, url)),
        });
        break;
      case "health":
        // **큐가 아직 안 왔으면 기다린다고 말한다.** 이 탭만 큐가 필요하다.
        if (queueErr) {
          renderError(body, queueErr);
        } else if (q) {
          renderHealth(body, q.health, backlogLine(q.confirm.length, q.retro.length));
        } else {
          body.replaceChildren(el("p", "empty", "상태를 읽는 중이다…"));
        }
        break;
    }
  }

  /** refresh 는 **빠른 것부터 그린다.**
   *
   * # 왜 갈랐나
   *
   * 예전에는 `Promise.all([fetchQueue(), fetchSettings()])` 로 둘을 같이 기다렸다.
   * 그런데 둘의 비용이 두 자릿수 배로 다르다 (2026-09-01 실측, 결정 560건):
   *
   *   prior settings --json   0.056초   ← 호스트·볼트 탭이 쓰는 전부
   *   prior queue    --json   6.3초     ← 상태 탭과 트레이 배지만 쓴다
   *
   * 그래서 호스트 탭만 보려 해도 6.3초를 기다렸다. 게다가 queue 비용은 **볼트
   * 크기에 비례해서 자란다** — 오늘 31.8초에서 6.3초로 줄였지만 다시 자란다.
   * 빠른 쪽에 느린 쪽을 묶어 두면 그 성장이 앱 전체의 체감이 된다.
   *
   * # 실패는 여전히 가른다
   *
   * 옛 주석의 걱정("설정이 안 읽히는데 상태만 그리면 사람은 앱이 멀쩡한 줄
   * 안다")은 그대로 지킨다 — **설정이 실패하면 화면 전체가 오류다.** 앱의
   * 알맹이가 그것이기 때문이다. 큐만 실패하면 상태 탭에서만 말한다.
   */
  async function refresh(): Promise<void> {
    // **설정 화면이 아니면 설정을 안 읽는다.** 볼트 화면은 `prior list` 하나로
    // 서는데, 여기서 settings·queue 를 기다리면 그 비용이 주역에 얹힌다.
    if (view === "browse") {
      drawBrowse();
      return;
    }
    let s: Settings;
    try {
      s = await fetchSettings();
    } catch (e) {
      const { body } = shell();
      renderError(body, e as CmdError);
      return;
    }
    // 아는 것으로 먼저 그린다. 두 번째 폴링부터는 lastQ 가 차 있어서
    // "읽는 중" 으로 되돌아가지 않는다.
    draw(lastQ, s);

    try {
      const q = await fetchQueue();
      lastQ = q;
      queueErr = null;
      await invoke("set_tray_title", { title: badgeText(q) });
    } catch (e) {
      queueErr = e as CmdError;
    }
    draw(lastQ, s);
  }

  // 첫 화면은 볼트다. 목록이 오기 전에도 껍데기를 그려 둔다 — 빈 화면보다
  // "무엇을 보는 자리인지" 가 먼저 보이는 편이 낫다.
  drawBrowse();
  void loadNotes().then(drawBrowse);

  // **트레이 배지는 계속 돈다.** 화면이 볼트여도 상태 검사는 알아야 한다.
  const tick = async (): Promise<void> => {
    try {
      const q = await fetchQueue();
      lastQ = q;
      queueErr = null;
      await invoke("set_tray_title", { title: badgeText(q) });
    } catch (e) {
      queueErr = e as CmdError;
    }
    if (view === "settings") void refresh();
  };
  // **첫 tick 을 늦춘다.** 트레이 배지는 급하지 않은데 `prior queue` 는 볼트
  // 크기에 비례해 자란다(2026-09-01 실측 6.3초). 이제 메인 스레드를 안 막지만,
  // 띄우자마자 프로세스를 하나 더 태우면 목록이 뜨는 속도를 뺏는다.
  setTimeout(() => void tick(), FIRST_TICK_MS);
  setInterval(() => void tick(), POLL_MS);
}

// **최상위에서 바로 돌지 않는다.** 그러면 이 모듈을 import 하는 것만으로 폴링이
// 시작되고, 시험에서 `#app` 이 없어 최상위가 터진다 — 실제로 그랬다.
const root = document.querySelector<HTMLDivElement>("#app");
if (root) start(root);
