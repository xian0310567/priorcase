import { el } from "./shell";

/** ── 마크다운 ────────────────────────────────────────────────────────────
 *
 * 라이브러리를 안 쓴다 — 이 앱의 런타임 의존은 0 이고(정적 Go 바이너리 + 바닐라
 * TS), 결정 노트가 쓰는 문법이 좁다.
 *
 * # 무엇을 지원할지는 재서 정했다
 *
 * 실볼트 결정 575건에서 문법별 사용 노트 수(2026-09-01):
 *
 *   제목 85% · 인라인코드 65% · 굵게 63% · 목록 61% · **중첩목록 57%** ·
 *   **위키링크 35%** · **표 33%** · 번호목록 23% · 코드블록 13% · 인용 8%
 *
 * 굵게 칠한 셋이 예전 렌더러가 못 그리던 것이다 — 중첩목록은 평평해졌고, 표는
 * 통째로 코드블록이 됐고, 위키링크는 못 눌렀다. 절반 넘는 노트가 그 셋 중 하나를
 * 쓰므로 "대충 보이면 된다" 로 넘길 자리가 아니었다.
 *
 * # HTML 을 만들지 않는다
 *
 * 문자열은 언제나 `textContent` 로 넣는다. 볼트 내용이 곧 마크업이 되면 결정문
 * 한 줄이 앱을 조작할 수 있다 — 이 앱은 남이 쓴 볼트(회사 볼트)도 연다.
 */

export interface MarkdownOptions {
  /** 위키링크를 누르면 부른다. 없으면 링크가 아니라 표시만 다른 글자가 된다. */
  onLink?: (stem: string) => void;
}

/** Block 은 그린 요소 하나와 그것이 온 **원문 줄 범위** [from, to) 다.
 *
 * # 왜 범위를 들고 다니나
 *
 * 옵시디언·노션은 "보기/편집 모드" 가 없다 — 누른 자리가 그 자리에서 원문이 되고
 * 나머지는 렌더된 채로 남는다. 그러려면 각 블록이 원문의 어디에서 왔는지를
 * 알아야 하고, 그 범위가 틀리면 고친 글이 **엉뚱한 줄을 덮어쓴다.**
 * 결정문이 조용히 망가지는 것은 되돌릴 수 없는 종류다. */
export interface Block {
  el: HTMLElement;
  from: number;
  to: number;
}

export interface Rendered {
  root: HTMLElement;
  blocks: Block[];
}

export function markdown(src: string, opt: MarkdownOptions = {}): Rendered {
  const root = el("article", "md");
  const lines = src.split("\n");
  const blocks: Block[] = [];
  let i = 0;

  /** push 는 블록 하나를 붙이면서 줄 범위를 새긴다. */
  const push = (node: HTMLElement, from: number, to: number): void => {
    node.dataset.from = String(from);
    node.dataset.to = String(to);
    root.append(node);
    blocks.push({ el: node, from, to });
  };

  while (i < lines.length) {
    const line = lines[i];

    if (line.startsWith("```")) {
      const start = i;
      const lang = line.slice(3).trim();
      const buf: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) buf.push(lines[i++]);
      if (i < lines.length) i++; // 닫는 울타리
      const pre = el("pre", "md-code");
      if (lang) pre.dataset.lang = lang;
      pre.append(el("code", "", buf.join("\n")));
      push(pre, start, i);
      continue;
    }

    // 들여쓴 코드 블록 — 탭이나 네 칸.
    //
    // **여기가 실측 표가 사는 자리다.** 결정문은 측정값을 파이프 표가 아니라
    // 탭으로 들여써 적는 일이 많고(실볼트에서 그 노트 하나에만 13줄), 정렬이
    // 곧 뜻이라 문단으로 뭉개면 숫자가 어느 열인지 알 수 없게 된다.
    //
    // 목록 항목·이어지는 줄과 헷갈리면 안 되므로 **앞이 빈 줄일 때만** 연다 —
    // 마크다운 규칙도 그렇고, 그래야 중첩 목록을 코드로 오독하지 않는다.
    if (isIndentedCode(line) && (i === 0 || lines[i - 1].trim() === "")) {
      const start = i;
      const buf: string[] = [];
      while (i < lines.length && (isIndentedCode(lines[i]) || lines[i].trim() === "")) {
        buf.push(dedent(lines[i++]));
      }
      // 뒤따라온 빈 줄은 이 블록의 것이 아니다 — 범위에서도 뺀다.
      while (buf.length && buf[buf.length - 1].trim() === "") {
        buf.pop();
        i--;
      }
      const pre = el("pre", "md-code md-code-plain");
      pre.append(el("code", "", buf.join("\n")));
      push(pre, start, i);
      continue;
    }

    const h = /^(#{1,6})\s+(.*)$/.exec(line);
    if (h) {
      push(inline(el(`h${Math.min(h[1].length + 1, 6)}`, "md-h"), h[2], opt), i, i + 1);
      i++;
      continue;
    }

    // 표 — 33% 가 쓴다. **실측 표가 많아서** 제대로 그려야 읽힌다.
    if (isTableRow(line) && i + 1 < lines.length && isDivider(lines[i + 1])) {
      const start = i;
      const rows: string[] = [];
      while (i < lines.length && isTableRow(lines[i])) rows.push(lines[i++]);
      push(table(rows, opt), start, i);
      continue;
    }

    if (listMark(line)) {
      const start = i;
      const buf: string[] = [];
      while (i < lines.length && (listMark(lines[i]) || isLazyContinuation(lines[i]))) buf.push(lines[i++]);
      push(list(buf, opt), start, i);
      continue;
    }

    if (line.startsWith(">")) {
      const start = i;
      const buf: string[] = [];
      while (i < lines.length && lines[i].startsWith(">")) buf.push(lines[i++].replace(/^>\s?/, ""));
      const q = el("blockquote", "md-quote");
      for (const b of buf) q.append(inline(el("p", "md-p"), b, opt));
      push(q, start, i);
      continue;
    }

    if (line.trim() === "") {
      i++;
      continue;
    }

    // 문단 — 빈 줄까지 이어 붙인다. 마크다운은 줄바꿈 하나를 문단 안의 공백으로 읽는다.
    //
    // **현재 줄을 무조건 먼저 먹는다.** 아래 조건으로만 돌리면, 어느 블록 분기도
    // 안 잡았는데 조건이 처음부터 거짓인 줄에서 `i` 가 안 움직여 무한 루프가 된다 —
    // 들여쓴 코드를 문단에서 제외하면서 실제로 그렇게 걸렸다(2026-09-01).
    const start = i;
    const para: string[] = [lines[i++]];
    while (
      i < lines.length &&
      lines[i].trim() !== "" &&
      !lines[i].startsWith("```") &&
      !/^#{1,6}\s/.test(lines[i]) &&
      !listMark(lines[i]) &&
      !lines[i].startsWith(">") &&
      !isTableRow(lines[i]) &&
      !isIndentedCode(lines[i])
    ) {
      para.push(lines[i++]);
    }
    push(inline(el("p", "md-p"), para.join(" ").trim(), opt), start, i);
  }
  return { root, blocks };
}

/** spliceLines 는 원문의 [from, to) 를 새 글로 갈아 끼운다.
 *
 * **블록 하나만 바꾼다.** 문서 전체를 다시 쓰지 않으므로 다른 자리의 글자·공백이
 * 한 바이트도 안 움직인다 — 볼트가 git 으로 오가는데 diff 가 고친 자리만 보여야
 * 무엇이 바뀌었는지 읽힌다. */
export function spliceLines(src: string, from: number, to: number, next: string): string {
  const lines = src.split("\n");
  const replacement = next === "" ? [] : next.split("\n");
  lines.splice(from, to - from, ...replacement);
  return lines.join("\n");
}

/** listMark 는 그 줄이 목록 항목인지와 들여쓰기 깊이를 준다. */
function listMark(line: string): { depth: number; ordered: boolean; text: string } | null {
  const m = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/.exec(line);
  if (!m) return null;
  // 탭은 4칸으로 센다. 이 볼트는 대개 두 칸씩 들여쓴다.
  const indent = m[1].replace(/\t/g, "    ").length;
  return { depth: Math.floor(indent / 2), ordered: /\d/.test(m[2]), text: m[3] };
}

/** 목록 항목이 다음 줄로 이어지는 경우 (표시 없이 들여쓴 줄). */
function isLazyContinuation(line: string): boolean {
  return /^\s+\S/.test(line) && !listMark(line);
}

/** list 는 **중첩을 살린다** — 57% 의 노트가 쓴다.
 *
 * 예전에는 깊이를 버리고 전부 한 층으로 폈다. 결정문은 "안이 셋" 같은 구조를
 * 자주 쓰는데, 평평해지면 그 층위가 사라져서 무엇이 무엇에 딸린 것인지 안 보인다. */
function list(lines: string[], opt: MarkdownOptions): HTMLElement {
  const first = listMark(lines[0]);
  const root = el(first?.ordered ? "ol" : "ul", "md-list");
  const stack: Array<{ depth: number; box: HTMLElement }> = [{ depth: first?.depth ?? 0, box: root }];
  let last: HTMLElement | null = null;

  for (const line of lines) {
    const m = listMark(line);
    if (!m) {
      // 이어지는 줄은 앞 항목에 붙인다.
      if (last) last.append(document.createTextNode(" " + line.trim()));
      continue;
    }
    while (stack.length > 1 && m.depth < stack[stack.length - 1].depth) stack.pop();
    if (m.depth > stack[stack.length - 1].depth) {
      const sub = el(m.ordered ? "ol" : "ul", "md-list md-sublist");
      (last ?? stack[stack.length - 1].box).append(sub);
      stack.push({ depth: m.depth, box: sub });
    }
    const li = inline(el("li", "md-li"), m.text, opt);
    stack[stack.length - 1].box.append(li);
    last = li;
  }
  return root;
}

/** isIndentedCode 는 탭이나 네 칸으로 시작하는 줄인지 본다. 목록 표시가 있으면 아니다. */
function isIndentedCode(line: string): boolean {
  if (!/^(\t|    )/.test(line)) return false;
  return !listMark(line);
}
function dedent(line: string): string {
  return line.replace(/^(\t|    )/, "");
}

function isTableRow(line: string): boolean {
  return line.trim().startsWith("|") && line.includes("|", 1);
}
function isDivider(line: string): boolean {
  return /^\s*\|?[\s:|-]+\|[\s:|-]*$/.test(line) && line.includes("-");
}
function cells(line: string): string[] {
  return line.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
}

/** table 은 진짜 표로 그린다 — 33% 가 쓰고, 그중 대부분이 **실측 표**다.
 *
 * 예전에는 코드블록으로 덤프했다. 정렬은 보존됐지만 열이 안 맞으면 읽을 수가
 * 없었고, 결정문의 표는 대개 "안 A/B/C 를 견주는 것" 이라 열 정렬이 곧 뜻이다. */
function table(rows: string[], opt: MarkdownOptions): HTMLElement {
  const t = el("table", "md-table");
  const head = el("thead", "");
  const tr = el("tr", "");
  for (const c of cells(rows[0])) tr.append(inline(el("th", "md-th"), c, opt));
  head.append(tr);
  t.append(head);

  const body = el("tbody", "");
  for (const row of rows.slice(2)) {
    const r = el("tr", "");
    for (const c of cells(row)) r.append(inline(el("td", "md-td"), c, opt));
    body.append(r);
  }
  t.append(body);
  const wrap = el("div", "md-tablewrap");
  wrap.append(t);
  return wrap;
}

/** inline 은 굵게·기울임·인라인코드·위키링크·링크를 요소로 바꾼다.
 *
 * **문자열은 언제나 textContent 다** (이 모듈의 §). */
function inline(host: HTMLElement, text: string, opt: MarkdownOptions): HTMLElement {
  const re = /(\*\*[^*]+\*\*|`[^`]+`|\[\[[^\]]+\]\]|\[[^\]]+\]\([^)]+\)|\*[^*\s][^*]*\*)/g;
  let last = 0;
  for (const m of text.matchAll(re)) {
    const at = m.index ?? 0;
    if (at > last) host.append(document.createTextNode(text.slice(last, at)));
    host.append(token(m[0], opt));
    last = at + m[0].length;
  }
  if (last < text.length) host.append(document.createTextNode(text.slice(last)));
  return host;
}

function token(tok: string, opt: MarkdownOptions): Node {
  if (tok.startsWith("**")) return el("strong", "", tok.slice(2, -2));
  if (tok.startsWith("`")) return el("code", "md-inline", tok.slice(1, -1));
  if (tok.startsWith("[[")) {
    // 위키링크 — 35% 가 쓴다. **누를 수 있어야 멘션이다.**
    // 별칭(`[[stem|보이는 글]]`)과 절 앵커(`[[stem#절]]`)를 벗긴다.
    const raw = tok.slice(2, -2);
    const stem = raw.split(/[|#]/)[0].trim();
    const label = raw.includes("|") ? raw.slice(raw.indexOf("|") + 1) : stem;
    if (!opt.onLink) return el("span", "md-wiki", label);
    const b = el("button", "md-wiki md-wiki-link", label);
    b.addEventListener("click", () => opt.onLink?.(stem));
    return b;
  }
  if (tok.startsWith("[")) {
    // 바깥 링크는 **누르게 두지 않는다.** 이 앱에는 브라우저를 여는 통로가 없고,
    // 있다 해도 볼트 내용이 여는 주소를 정하는 것은 열어 둘 문이 아니다.
    const label = tok.slice(1, tok.indexOf("]"));
    return el("span", "md-extlink", label);
  }
  return el("em", "", tok.slice(1, -1));
}
