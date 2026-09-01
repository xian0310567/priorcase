import type { NoteFull } from "../types";
import { el } from "./shell";

/** ── 속성 패널 ──────────────────────────────────────────────────────────
 *
 * frontmatter 를 옵시디언처럼 **구조화된 표**로 보여 준다.
 *
 * # 왜 필요한가
 *
 * 결정 노트의 알맹이 절반이 frontmatter 에 있다 — 언제·누가·어느 도메인·상태·
 * 결과·무엇을 뒤집었나. 본문만 그리면 그 절반이 안 보이고, 사람은 "왜 이걸
 * 결정이라 부르나" 를 화면에서 알 수 없다.
 *
 * # 값이 없어도 줄은 남긴다
 *
 * `supersedes` 가 비었다는 것은 **정보다** — "이건 무엇도 뒤집지 않았다". 줄을
 * 통째로 빼면 그 사실이 "속성이 없다" 와 구별되지 않는다. 옵시디언도 "값 없음"
 * 으로 남긴다.
 *
 * # 여기서 못 고친다
 *
 * 앱의 편집은 본문까지다(2026-09-01 결정). frontmatter 는 스키마라 `prior review`
 * 로만 간다 — 화면이 고칠 수 있는 것처럼 보이면 안 되므로 입력이 아니라 글로 그린다.
 */

/** 옵시디언의 속성 아이콘을 흉내 낸 글리프. 타입을 한눈에 가르는 용도다. */
const ICON: Record<string, string> = {
  text: "≡",
  date: "▤",
  list: "☰",
  tags: "⌗",
};

interface Row {
  key: string;
  icon: string;
  value: string | string[];
  /** pill 이면 값을 알약으로 그린다 (tags·domain). */
  pill?: boolean;
}

/** arr 는 배열 속성을 **믿지 않고** 읽는다.
 *
 * # 왜 방어하나
 *
 * 앱은 번들된 prior 를 폴백으로 쓰고 PATH 의 npm 판을 우선한다(commands.rs 의
 * prior_bin). **둘의 판이 같을 이유가 없다** — 앱을 새로 받았는데 PATH 의 prior 가
 * 옛것이거나, 그 반대다. 그러면 `show --json` 이 새 키를 안 낸다.
 *
 * 2026-09-01 에 실제로 그랬다: 번들된 prior 가 `summary_history` 를 안 냈는데
 * 이 파일이 `.length` 를 무조건 읽어 TypeError 를 냈고, 그 예외가 렌더를 통째로
 * 끊어 **화면이 검게 비었다.** 판 차이는 현장에서 반드시 일어나므로 여기서
 * 막아야 한다 — 없는 값은 "값 없음" 이지 고장이 아니다. */
function arr(v: unknown): string[] {
  return Array.isArray(v) ? v.map(String) : [];
}
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

export function noteRows(n: NoteFull): Row[] {
  return [
    { key: "type", icon: ICON.text, value: str(n.type) },
    { key: "date", icon: ICON.date, value: str(n.date) },
    { key: "author", icon: ICON.text, value: str(n.author) },
    { key: "domain", icon: ICON.list, value: arr(n.domain), pill: true },
    { key: "vault", icon: ICON.text, value: str(n.vault) },
    { key: "summary", icon: ICON.text, value: str(n.summary) },
    { key: "status", icon: ICON.text, value: str(n.status) },
    { key: "outcome", icon: ICON.text, value: str(n.outcome) },
    { key: "supersedes", icon: ICON.list, value: n.supersedes },
    { key: "superseded_reason", icon: ICON.text, value: str(n.superseded_reason) },
    { key: "related", icon: ICON.list, value: n.related },
    { key: "tags", icon: ICON.tags, value: arr(n.tags), pill: true },
    { key: "source_session", icon: ICON.text, value: str(n.source_session) },
  ];
}

/** ReviewPatch 는 `prior review` 에 넘길 조각이다. 준 것만 바뀐다. */
export interface ReviewPatch {
  summary?: string;
  status?: string;
  outcome?: string;
  retro?: string;
  tags?: string[];
}

export interface PropertyActions {
  /** 위키링크(supersedes·related)를 누르면 부른다. */
  open: (stem: string) => void;
  /** 속성을 고쳤을 때 부른다. */
  review: (patch: ReviewPatch) => void;
}

/** ── 무엇을 열고 무엇을 잠그나 ──────────────────────────────────────────
 *
 * 통로가 `prior review` 라 그것이 아는 필드만 열린다. 나머지를 잠그는 이유는
 * 각각 다르다:
 *
 *   date·domain   파일명이 그 둘을 담는다(`{domain}-결정-{slug}-{date}`) — 고치면
 *                 파일이 옮겨져야 하고, 그건 이 볼트의 위키링크를 전부 끊는다.
 *   type          이 노트가 결정이라는 사실 자체다. 바꾸면 회수 대상에서 빠진다.
 *   author        누가 정했나는 기록된 사실이지 판단이 아니다.
 *   source_session 어느 대화에서 나왔나 — 같은 이유.
 *   vault         도메인이 정한다(domain bind). 여기서 고치면 두 곳이 갈린다.
 *   supersedes    `--reason` 이 같이 필요하다 — 한 칸짜리 편집으로 낼 수 없다.
 *   related       덧붙이기만 되고 빼기가 안 된다(capture.Review) — 반쪽만 열면
 *                 지울 수 있다고 오해한다.
 *
 * **못 고치는 칸이 고칠 수 있는 것처럼 보이는 쪽이 더 나쁘다.** 그래서 열리는
 * 것만 손이 닿고 나머지는 아무 반응이 없다. */
export const editableKeys = ["summary", "status", "outcome", "tags"] as const;

/** 고르는 칸의 허용값 — CLI 가 받는 것과 같아야 한다. 여기서 갈리면 화면이
 * 받아 놓고 명령이 거절하는데, 그 오류는 사람이 자기 탓으로 읽는다. */
const CHOICES: Record<string, string[]> = {
  status: ["active", "superseded", "regretted", "retracted"],
  outcome: ["pending", "good", "bad"],
};

export function renderProperties(root: HTMLElement, n: NoteFull, on: PropertyActions): void {
  const box = el("section", "props");
  box.append(el("h2", "props-title", "속성"));
  const grid = el("div", "props-grid");

  for (const r of noteRows(n)) {
    grid.append(el("div", "props-key", `${r.icon}  ${r.key}`));
    grid.append(valueCell(r, n, on));
  }
  box.append(grid);

  // 요약이 바뀐 적이 있으면 옛 요약도 보여 준다 — 회수가 주입하는 것이 그 한 줄이라
  // 무엇이 바뀌었는지가 곧 "회수가 무엇을 말하게 됐나" 다.
  const history = arr(n.summary_history);
  if (history.length) {
    const hist = el("details", "props-history");
    hist.append(el("summary", "props-history-head", `옛 요약 ${history.length}개`));
    for (const h of history) hist.append(el("p", "props-history-item", h));
    box.append(hist);
  }
  root.append(box);
}

function valueCell(r: Row, n: NoteFull, on: PropertyActions): HTMLElement {
  const editable = (editableKeys as readonly string[]).includes(r.key);
  if (editable && CHOICES[r.key]) return choiceCell(r, n, on);
  if (editable) return textCell(r, on);

  const cell = el("div", "props-val props-locked");
  if (Array.isArray(r.value)) {
    if (r.value.length === 0) {
      cell.append(el("span", "props-empty", "값 없음"));
      return cell;
    }
    for (const v of r.value) {
      const stem = v.replace(/^\[\[/, "").replace(/\]\]$/, "");
      if (r.pill) {
        cell.append(el("span", "props-pill", stem));
      } else {
        // supersedes·related 는 **누를 수 있어야 한다** — 뒤집힌 사슬을 따라가는
        // 것이 이 도구를 쓰는 이유 중 하나다.
        const b = el("button", "props-link", stem);
        b.addEventListener("click", () => on.open(stem));
        cell.append(b);
      }
    }
    return cell;
  }
  if (!r.value) {
    cell.append(el("span", "props-empty", "값 없음"));
    return cell;
  }
  cell.append(el("span", "props-text", r.value));
  return cell;
}

/** choiceCell 은 허용값이 정해진 칸이다 (status·outcome).
 *
 * **자유 입력이 아니다.** 오타 하나가 그 노트를 회수에서 빼거나(retracted) 상태를
 * 뜻 없는 값으로 만든다. 고르게 하면 그 실수가 불가능해진다. */
function choiceCell(r: Row, n: NoteFull, on: PropertyActions): HTMLElement {
  const cell = el("div", "props-val");
  const sel = document.createElement("select");
  sel.className = "props-select";
  sel.setAttribute("aria-label", r.key);
  for (const v of CHOICES[r.key]) {
    const o = document.createElement("option");
    o.value = v;
    o.textContent = v;
    o.selected = v === r.value;
    sel.append(o);
  }
  const warn = el("p", "props-warn");
  sel.addEventListener("change", () => {
    // **철회에는 이유가 필요하다** (capture.Review 가 요구한다). 화면에서 먼저
    // 막는다 — 명령이 거절하는 것을 보여 주면 사람은 자기가 뭘 잘못했는지 모른다.
    if (r.key === "status" && sel.value === "retracted" && !n.body.includes("## 회고")) {
      warn.textContent =
        "철회하려면 이유가 먼저다 — 본문에 ## 회고 를 쓰고 왜 결정이 아닌지 남겨라. 회수에서 빠지므로 나중에 아무도 못 묻는다.";
      sel.value = String(r.value);
      return;
    }
    warn.textContent = "";
    on.review({ [r.key]: sel.value });
  });
  cell.append(sel, warn);
  return cell;
}

/** textCell 은 눌러서 고치는 칸이다 (summary·tags).
 *
 * 본문 블록과 같은 규약이다 — 누르면 그 자리가 원문이 되고, 포커스를 잃으면
 * 저장하며, 안 바뀌었으면 아무것도 안 한다. */
function textCell(r: Row, on: PropertyActions): HTMLElement {
  const cell = el("div", "props-val props-editable");
  const isTags = r.key === "tags";
  const shown = isTags ? (r.value as string[]).filter((t) => t.toLowerCase() !== "decision") : [];
  const raw = isTags ? shown.join(", ") : String(r.value ?? "");

  const view = el("div", "props-view");
  if (isTags) {
    if (shown.length === 0) view.append(el("span", "props-empty", "값 없음"));
    for (const t of shown) view.append(el("span", "props-pill", t));
  } else {
    view.append(raw ? el("span", "props-text", raw) : el("span", "props-empty", "값 없음"));
  }

  cell.addEventListener("click", () => {
    if (cell.querySelector("textarea")) return;
    const ta = document.createElement("textarea");
    ta.className = "props-editor";
    ta.value = raw;
    ta.rows = 1;
    ta.spellcheck = false;
    const fit = (): void => {
      ta.style.height = "auto";
      ta.style.height = `${ta.scrollHeight}px`;
    };
    ta.addEventListener("input", fit);
    ta.addEventListener("blur", () => {
      const next = ta.value.trim();
      if (next === raw.trim()) {
        cell.replaceChildren(view);
        return;
      }
      if (isTags) {
        // **빈 목록도 보낸다** — 그것이 "전부 지운다" 라, 안 보내는 것과 달라야 한다.
        on.review({ tags: next ? next.split(",").map((t) => t.trim()).filter(Boolean) : [] });
      } else {
        on.review({ [r.key]: next });
      }
    });
    ta.addEventListener("keydown", (e) => {
      const k = e as KeyboardEvent;
      if (k.key === "Escape") {
        k.preventDefault();
        cell.replaceChildren(view);
      }
    });
    cell.replaceChildren(ta);
    queueMicrotask(() => {
      ta.focus();
      ta.setSelectionRange(ta.value.length, ta.value.length);
      fit();
    });
  });
  cell.append(view);
  return cell;
}
