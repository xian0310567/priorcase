import type { HostInfo } from "../types";
import { hostState } from "../format";
import { el } from "./shell";

export interface HostActions {
  toggle: (name: string, enabled: boolean) => void;
}

/** renderHosts 는 **어느 도구의 대화를 훑을지** 고르는 화면이다.
 *
 * # 왜 목록이 레지스트리에서 오나
 *
 * 설정에 적힌 것만 보여 주면, 새 파서를 붙였을 때 기존 사용자는 그 존재를
 * 영영 모른다. `prior settings` 가 레지스트리 전부를 주고 켜짐 여부를 함께
 * 단다 — 꺼진 것도 보여야 다시 켤 수 있다.
 *
 * # 끄면 무엇을 잃는가
 *
 * 그 도구에서 내린 결정은 안 잡힌다. 이미 기록된 노트는 그대로 있다. 그
 * 사실을 줄마다 적지는 않는다 — 화면이 설명으로 덮이면 정작 상태가 안 보인다.
 */
export function renderHosts(root: HTMLElement, items: HostInfo[], on: HostActions): void {
  root.replaceChildren();
  if (items.length === 0) {
    // **빈 목록은 고장이다.** 레지스트리에는 언제나 최소 하나가 있다.
    root.append(el("p", "empty", "호스트 목록을 받지 못했다. prior 가 도는지 확인하라."));
    return;
  }
  for (const h of items) {
    const row = el("div", `host-row${h.enabled ? "" : " off"}`);

    const box = document.createElement("input");
    box.type = "checkbox";
    box.className = "host-toggle";
    box.checked = h.enabled;
    // 사람이 읽을 이름을 붙인다 — 체크박스만 있으면 스크린리더에서 무명이다.
    box.setAttribute("aria-label", h.name);
    box.addEventListener("change", () => {
      // **낙관적으로 그리지 않는다.** 여기서 화면을 먼저 바꾸면 CLI 가 거부했을
      // 때(모르는 이름·설정 오류) 화면과 설정이 갈린다. 다시 읽어서 그린다.
      box.disabled = true;
      on.toggle(h.name, box.checked);
    });

    const name = el("span", "host-name", h.name);
    const state = el("span", "host-state", hostState(h));
    const root_ = el("div", "host-root", h.root || "기본 자리를 못 찾았다");

    row.append(box, name, state, root_);
    root.append(row);
  }
}
