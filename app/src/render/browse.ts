import type { NoteRow, NoteFull, SearchRow, CmdError } from "../types";
import { el, renderError } from "./shell";
import { markdown } from "./markdown";
import { renderProperties, type ReviewPatch } from "./properties";

/** ── 볼트 브라우저 ──────────────────────────────────────────────────────
 *
 * 세 칸이다: 도메인 → 결정 목록 → 본문.
 *
 * # 왜 세 칸인가
 *
 * 두 칸(목록↔본문 드릴다운)이면 결정을 하나 열 때마다 목록이 사라진다. 이 볼트는
 * 한 도메인에 결정이 60건 넘게 있어서 **옆 것과 견주며 읽는 일**이 잦다 — 무엇을
 * 정했는지보다 "그 앞뒤로 무엇이 있었나" 가 중요할 때가 많다.
 *
 * # 검색은 필터가 아니다
 *
 * 이름으로 거르는 것이 아니라 **회수 랭킹**을 쓴다(`prior recall --format json`).
 * 옵시디언이 못 하는 것이 이것이고, 그래서 검색 결과에는 점수를 같이 보여 준다 —
 * 왜 이 순서인지가 안 보이면 사람은 순위를 안 믿는다.
 */

export interface BrowseState {
  notes: NoteRow[];
  domain: string | null;
  selected: string | null;
  open: NoteFull | null;
  query: string;
  results: SearchRow[] | null;
  /** 지금 원문으로 펼쳐 둔 블록의 줄 범위. null 이면 전부 렌더된 상태다.
   *
   * **모드가 없다** — 옵시디언·노션처럼 누른 자리만 원문이 되고 나머지는 글로
   * 남는다. 예전에는 "본문 고치기" 버튼으로 문서 전체를 textarea 로 바꿨는데,
   * 그건 읽던 자리를 잃게 만들고 무엇을 고치는지 맥락이 사라진다. */
  editingBlock: { from: number; to: number } | null;
  draft: string;
  saving: boolean;
  /** 마지막 실패. **조용히 넘기지 않는다** — 0건과 고장이 같아 보이면 안 된다. */
  err: CmdError | null;
}

export interface BrowseActions {
  pickDomain: (d: string | null) => void;
  pickNote: (stem: string) => void;
  search: (q: string) => void;
  /** 블록을 눌러 원문으로 펼친다. */
  editBlock: (from: number, to: number, text: string) => void;
  cancelEdit: () => void;
  changeDraft: (v: string) => void;
  /** 펼친 블록을 원문에 갈아 끼우고 저장한다. */
  commitBlock: () => void;
  /** 속성을 고친다 — 본문과 달리 `prior review` 를 탄다 (properties.ts 의 §). */
  review: (patch: ReviewPatch) => void;
  openSettings: () => void;
}

/** domainCounts 는 도메인별 결정 수다. 한 노트가 도메인 여럿에 속할 수 있다 —
 * `domain` 배열이 회수 경로이기 때문이다(볼트 규약). 그래서 합이 총계보다 크다. */
export function domainCounts(notes: NoteRow[]): Array<[string, number]> {
  const m = new Map<string, number>();
  for (const n of notes) {
    for (const d of n.domain.length ? n.domain : ["(없음)"]) {
      m.set(d, (m.get(d) ?? 0) + 1);
    }
  }
  return [...m].sort((a, b) => (b[1] - a[1]) || a[0].localeCompare(b[0]));
}

/** statusMark 는 결정의 상태를 한 글자로 준다.
 *
 * **뒤집힌 것과 아쉬운 것은 반드시 보여야 한다.** 그것을 모르고 읽으면 이미
 * 폐기된 결론을 지금 것으로 읽는다 — 이 도구가 막으려는 바로 그 일이다. */
export function statusMark(n: { status: string; outcome: string }): { text: string; cls: string } {
  if (n.status === "superseded") return { text: "뒤집힘", cls: "mark-superseded" };
  if (n.status === "retracted") return { text: "철회", cls: "mark-retracted" };
  if (n.outcome === "bad" || n.status === "regretted") return { text: "아쉬움", cls: "mark-bad" };
  if (n.outcome === "good") return { text: "좋음", cls: "mark-good" };
  return { text: "", cls: "" };
}

/** viewKey 는 "지금 어느 화면인가" 다. 스크롤을 지킬지 되돌릴지를 이것으로 가른다.
 *
 * 같은 글을 다시 그리는 것(고치고 저장하기)과 다른 글로 가는 것은 사람에게 전혀
 * 다른 일이다. 앞은 읽던 자리를 지켜야 하고, 뒤는 **맨 위에서 시작해야 한다** —
 * 새 글이 중간부터 보이면 원래 고장보다 더 헷갈린다. */
function viewKey(s: BrowseState): string {
  const stem = s.open?.stem ?? s.selected;
  if (stem) return `note:${stem}`;
  if (s.results) return `search:${s.query}`;
  return `list:${s.domain ?? ""}`;
}

/** renderBrowse 는 볼트 화면을 그린다.
 *
 * # 읽던 자리를 지킨다
 *
 * 2026-09-01 재현: 긴 결정문을 아래까지 읽다가 한 문단을 눌러 고치고, 마치려고
 * 다른 데를 누르면 **화면이 맨 위로 튀었다.** 고친 자리를 다시 찾아야 했다.
 *
 * 원인은 이 함수다. 상태가 바뀔 때마다 화면을 통째로 다시 만드는데 `.content` 가
 * 새 요소라 스크롤이 0에서 시작한다. 저장은 상태를 두 번 바꾸므로(saving → 완료)
 * 두 번 튄다.
 *
 * 그래서 **다시 만들기 전에 스크롤을 읽어 두고 같은 화면이면 되돌려 놓는다.**
 * 부르는 쪽이 `root` 를 매번 새로 만들면 읽을 자리가 없으므로, main.ts 는 껍데기를
 * 재사용한다. 직전 화면이 무엇이었는지는 `root` 에 적어 둔다 — 모듈 전역에 두면
 * 화면이 둘 이상일 때 서로를 덮는다.
 *
 * 사이드바는 **언제나** 지킨다. 글을 바꿔도 목록에서 보던 자리는 그대로여야 한다. */
export function renderBrowse(root: HTMLElement, s: BrowseState, on: BrowseActions): void {
  const prevKey = root.dataset.view ?? "";
  const prevTop = root.querySelector<HTMLElement>(".content")?.scrollTop ?? 0;
  const prevSideTop = root.querySelector<HTMLElement>(".side")?.scrollTop ?? 0;
  const key = viewKey(s);

  root.replaceChildren();
  const main = el("main", "content");
  // **노션은 두 칸이다** — 사이드바와 본문. 목록은 세 번째 칸이 아니라 본문
  // 자리에 뜨는 하나의 "뷰" 이고, 결정을 고르면 그 자리가 글로 바뀐다.
  //
  // 예전에 세 칸으로 만들었던 이유(옆 것과 견주며 읽기)는 브레드크럼으로 갚는다 —
  // 목록으로 돌아가는 길이 늘 한 번의 클릭이면 맥락이 안 끊긴다.
  if (s.open || s.selected) main.append(readerPane(s, on));
  else main.append(listPane(s, on));
  const side = sidebar(s, on);
  root.append(side, main);

  root.dataset.view = key;
  side.scrollTop = prevSideTop;
  if (key === prevKey) main.scrollTop = prevTop;
}

function sidebar(s: BrowseState, on: BrowseActions): HTMLElement {
  const side = el("aside", "side");

  const box = el("div", "side-search");
  const input = document.createElement("input");
  input.type = "search";
  input.className = "side-search-input";
  input.placeholder = "결정 검색";
  input.value = s.query;
  // **입력마다 부르지 않는다.** 회수는 볼트 전체를 훑는다 — 한 글자마다 돌리면
  // 타자가 밀린다. 엔터에서만 부른다.
  input.addEventListener("keydown", (e) => {
    if ((e as KeyboardEvent).key === "Enter") on.search(input.value.trim());
  });
  box.append(input);
  side.append(box);

  const nav = el("nav", "side-nav");
  const all = el("button", `side-item${s.domain === null && !s.results ? " active" : ""}`);
  all.append(el("span", "side-item-name", "전체"), el("span", "side-item-count", String(s.notes.length)));
  all.addEventListener("click", () => on.pickDomain(null));
  nav.append(all);

  for (const [d, n] of domainCounts(s.notes)) {
    const b = el("button", `side-item${s.domain === d && !s.results ? " active" : ""}`);
    b.append(el("span", "side-item-name", d), el("span", "side-item-count", String(n)));
    b.addEventListener("click", () => on.pickDomain(d));
    nav.append(b);
  }
  side.append(nav);

  // 설정은 **맨 아래 톱니바퀴** 하나다. 처음 한 번 만지고 안 보는 것이라
  // 상시 노출될 이유가 없고, 그래야 사이드바가 온전히 볼트 구조에 쓰인다.
  const gear = el("button", "side-settings", "⚙  설정");
  gear.addEventListener("click", () => on.openSettings());
  side.append(gear);
  return side;
}

function listPane(s: BrowseState, on: BrowseActions): HTMLElement {
  const pane = el("section", "list");
  const rows: Array<NoteRow | SearchRow> = s.results
    ? s.results
    : s.notes.filter((n) => s.domain === null || (n.domain ?? []).includes(s.domain));

  const head = el("div", "list-head");
  head.append(el("span", "list-head-title", s.results ? `"${s.query}" 검색 결과` : (s.domain ?? "전체")));
  head.append(el("span", "list-head-count", `${rows.length}건`));
  pane.append(head);

  if (s.err) {
    const box = el("div", "list-error");
    renderError(box, s.err);
    pane.append(box);
    return pane;
  }
  if (rows.length === 0) {
    pane.append(el("p", "empty", s.results ? "걸리는 결정이 없다." : "결정이 없다."));
    return pane;
  }
  const scroll = el("div", "list-scroll");
  for (const n of rows) {
    const row = el("button", `list-row${n.stem === s.selected ? " active" : ""}`);
    const top = el("div", "list-row-top");
    top.append(el("span", "list-row-date", n.date));
    for (const d of n.domain ?? []) top.append(el("span", "list-row-domain", d));
    const mk = statusMark(n);
    if (mk.text) top.append(el("span", `list-row-mark ${mk.cls}`, mk.text));
    if ("score" in n) top.append(el("span", "list-row-score", String(n.score)));
    row.append(top, el("div", "list-row-summary", n.summary || n.stem));
    row.addEventListener("click", () => on.pickNote(n.stem));
    scroll.append(row);
  }
  pane.append(scroll);
  return pane;
}

function readerPane(s: BrowseState, on: BrowseActions): HTMLElement {
  const pane = el("section", "reader");
  const n = s.open;
  if (!n) {
    if (s.err) {
      renderError(pane, s.err);
    } else {
      pane.append(el("p", "empty", "읽는 중이다…"));
    }
    return pane;
  }

  const crumb = el("nav", "crumb");
  const back = el("button", "crumb-link", s.results ? `"${s.query}" 검색 결과` : (s.domain ?? "전체"));
  back.addEventListener("click", () => on.pickDomain(s.results ? s.domain : s.domain));
  crumb.append(back, el("span", "crumb-sep", "/"), el("span", "crumb-here", n.stem));
  pane.append(crumb);

  const head = el("header", "reader-head");
  const meta = el("div", "reader-meta");
  meta.append(el("span", "reader-date", n.date));
  for (const d of n.domain ?? []) meta.append(el("span", "reader-domain", d));
  const mk = statusMark(n);
  if (mk.text) meta.append(el("span", `reader-mark ${mk.cls}`, mk.text));
  if (n.author) meta.append(el("span", "reader-author", n.author));
  head.append(meta, el("h1", "reader-title", n.summary || n.stem));

  // **무엇이 뒤집었는지는 본문보다 먼저 읽혀야 한다.** 뒤집힌 줄 모르고 읽으면
  // 폐기된 결론을 지금 것으로 읽는다.
  if (n.superseded_reason) {
    head.append(el("p", "reader-overturn", `뒤집혔다 — ${n.superseded_reason}`));
  }
  pane.append(head);

  // 편집 바가 없다. **모드가 없기 때문이다** — 아래 블록을 누르면 그 자리가 원문이 된다.
  if (s.saving) pane.append(el("div", "reader-saving", "저장 중…"));

  // 속성이 본문보다 먼저다 — 옵시디언과 같은 순서이고, 결정 노트는 알맹이
  // 절반이 frontmatter 에 있다 (properties.ts 의 §).
  renderProperties(pane, n, {
    open: (stem) => on.pickNote(stem),
    review: (patch) => on.review(patch),
  });
  pane.append(bodyView(n, s, on));

  const sup = n.supersedes ?? [];
  const rel = n.related ?? [];
  if (sup.length || rel.length) {
    const links = el("footer", "reader-links");
    if (sup.length) links.append(linkList("뒤집은 결정", sup, on));
    if (rel.length) links.append(linkList("관련", rel, on));
    pane.append(links);
  }
  return pane;
}

/** bodyView 는 본문을 그린다. **누른 블록만 원문으로 펼친다.**
 *
 * # 왜 블록 단위인가
 *
 * 문서 전체를 textarea 로 바꾸면 읽던 자리를 잃고, 무엇을 고치는지 맥락이
 * 사라진다. 옵시디언은 커서가 있는 줄만 원문으로 보이고 노션은 블록마다 따로
 * 편집한다 — 둘 다 "지금 손대는 곳" 만 날것이다.
 *
 * 그리고 안전하다: 고친 것을 원문의 **그 줄 범위에만** 갈아 끼우므로 다른 자리의
 * 글자가 한 바이트도 안 움직인다(spliceLines 의 §). 볼트가 git 으로 오가는데
 * diff 가 고친 자리만 보여야 무엇이 바뀌었는지 읽힌다. */
function bodyView(n: NoteFull, s: BrowseState, on: BrowseActions): HTMLElement {
  const src = n.body ?? "";
  const { root, blocks } = markdown(src, { onLink: (stem) => on.pickNote(stem) });
  const lines = src.split("\n");

  for (const b of blocks) {
    const editing =
      s.editingBlock !== null && s.editingBlock.from === b.from && s.editingBlock.to === b.to;
    if (editing) {
      b.el.replaceWith(blockEditor(lines.slice(b.from, b.to).join("\n"), s, on, blockKind(b.el)));
      continue;
    }
    b.el.classList.add("md-block");
    b.el.addEventListener("click", (e) => {
      // 위키링크·바깥 링크를 눌렀으면 그건 이동이지 편집이 아니다.
      if ((e.target as HTMLElement).closest("button")) return;
      on.editBlock(b.from, b.to, lines.slice(b.from, b.to).join("\n"));
    });
  }

  // 문서 끝에 새 블록을 더하는 자리. 노션의 "빈 줄 누르기" 와 같다.
  //
  // **빈 범위(from === to)는 어떤 블록과도 안 맞는다.** 위 루프가 못 잡으므로
  // 여기서 따로 그린다 — 안 그리면 눌러도 아무 일이 안 난다.
  const appending =
    s.editingBlock !== null && s.editingBlock.from === s.editingBlock.to;
  if (appending) {
    root.append(blockEditor("", s, on, "text"));
  } else {
    const tail = el("div", "md-append", "+  여기에 쓰기");
    tail.addEventListener("click", () => on.editBlock(lines.length, lines.length, ""));
    root.append(tail);
  }
  return root;
}

/** blockKind 는 그 블록이 어떤 글이었는지다 — 편집기가 **같은 모양**을 유지하려고 쓴다. */
function blockKind(node: HTMLElement): string {
  if (node.tagName === "PRE") return "code";
  if (/^H[1-6]$/.test(node.tagName)) return node.tagName.toLowerCase();
  return "text";
}

/** blockEditor 는 펼친 블록의 원문 상자다.
 *
 * # 모양을 안 바꾼다
 *
 * **옵시디언은 편집에 들어가도 글의 모양이 그대로다** — 상자도 테두리도 없고
 * 글꼴도 안 바뀐다. 보이는 것이 달라지면 "지금은 다른 화면" 이라는 느낌이 들고,
 * 그건 우리가 없앤 모드가 다시 생기는 것과 같다.
 *
 * 그래서 대체한 블록의 종류를 받아 그 모양을 그대로 쓴다 — 코드 블록은 고정폭,
 * 제목은 제목 크기, 나머지는 본문 글꼴이다.
 *
 * 저장은 **포커스를 잃을 때**다 (노션과 같다). Esc 는 되돌리고, ⌘↵ 는 바로 저장한다. */
function blockEditor(text: string, s: BrowseState, on: BrowseActions, kind: string): HTMLElement {
  const ta = document.createElement("textarea");
  ta.className = `md-editor md-editor--${kind}`;
  ta.value = s.draft;
  ta.spellcheck = false;
  ta.rows = Math.max(2, text.split("\n").length + 1);
  // 타자마다 다시 그리지 않는다 — 커서가 튄다. 초안만 기억한다.
  ta.addEventListener("input", () => {
    on.changeDraft(ta.value);
    ta.style.height = "auto";
    ta.style.height = `${ta.scrollHeight}px`;
  });
  ta.addEventListener("blur", () => on.commitBlock());
  ta.addEventListener("keydown", (e) => {
    const k = e as KeyboardEvent;
    if (k.key === "Escape") {
      k.preventDefault();
      on.cancelEdit();
    } else if (k.key === "Enter" && (k.metaKey || k.ctrlKey)) {
      k.preventDefault();
      on.commitBlock();
    }
  });
  // 그린 직후 커서를 준다 — 누른 사람은 이미 거기를 고칠 생각이다.
  queueMicrotask(() => {
    // **preventScroll 이다.** 누른 블록은 이미 화면 안에 있고, focus 가 스스로
    // 굴리면 방금 되돌려 놓은 스크롤(renderBrowse 의 §)을 다시 흐트러뜨린다.
    ta.focus({ preventScroll: true });
    ta.setSelectionRange(ta.value.length, ta.value.length);
    ta.style.height = "auto";
    ta.style.height = `${ta.scrollHeight}px`;
  });
  return ta;
}

function linkList(label: string, stems: string[], on: BrowseActions): HTMLElement {
  const box = el("div", "reader-linkgroup");
  box.append(el("span", "reader-linklabel", label));
  for (const raw of stems) {
    const stem = raw.replace(/^\[\[/, "").replace(/\]\]$/, "");
    const a = el("button", "reader-link", stem);
    a.addEventListener("click", () => on.pickNote(stem));
    box.append(a);
  }
  return box;
}
