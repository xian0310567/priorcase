import type { Settings } from "../types";
import { vaultState, vaultOfDomain } from "../format";
import { el } from "./shell";

export interface VaultActions {
  open: (name: string) => void;
  add: (name: string) => void;
  bind: (prefix: string, vault: string) => void;
  remote: (name: string, url: string) => void;
}

/** DEFAULT_VAULT 는 Go 쪽 config.DefaultVaultName 과 같아야 한다.
 *
 * 도메인의 vault 가 비면 이 볼트로 간다. 값이 어긋나면 화면이 "엮이지 않았다"
 * 고 말하는데 실제로는 잘 가고 있는 상태가 된다. */
export const DEFAULT_VAULT = "default";

/** renderVaults 는 **볼트와 프로젝트 연결**을 다루는 화면이다.
 *
 * 두 덩이다:
 *   1. 볼트 목록 — 어디에 있고, 결정이 몇 건이고, 누가 쓰는가
 *   2. 프로젝트 → 볼트 — 각 프로젝트의 결정이 어느 볼트로 가는가
 *
 * # 왜 한 화면인가
 *
 * 둘을 나누면 "볼트를 만들었는데 아무 프로젝트도 안 쓴다" 는 상태가 어느
 * 화면에서도 안 보인다. 그건 흔한 실수이고 조용하다 — 볼트는 만들어졌고
 * 기록은 전부 옛 볼트로 계속 간다. */
export function renderVaults(root: HTMLElement, s: Settings, on: VaultActions): void {
  root.replaceChildren();

  root.append(el("h3", "section-title", "볼트"));
  if (s.vaults.length === 0) {
    root.append(el("p", "empty", "볼트가 하나도 없다. 아래에서 만들어라."));
  }
  for (const v of s.vaults) {
    const row = el("div", `vault-row${v.exists ? "" : " broken"}`);
    row.append(el("span", "vault-name", v.name), el("span", "vault-state", vaultState(v)));
    row.append(el("div", "vault-path", v.path));

    const openBtn = document.createElement("button");
    openBtn.className = "btn";
    openBtn.textContent = "열기";
    // **자리가 없으면 못 연다.** 눌러서 아무 일도 안 나는 버튼은 앱이 고장난
    // 것처럼 보이게 한다.
    openBtn.disabled = !v.exists;
    openBtn.addEventListener("click", () => on.open(v.name));
    row.append(openBtn);
    row.append(remoteField(v, on));
    root.append(row);
  }

  root.append(addVaultForm(s.vault_parent, on));

  root.append(el("h3", "section-title", "프로젝트 → 볼트"));
  if (s.domains.length === 0) {
    root.append(el("p", "empty", "설정에 프로젝트가 없다."));
  }
  const names = s.vaults.map((v) => v.name);
  for (const d of s.domains) {
    const row = el("div", "domain-row");
    row.append(el("span", "domain-prefix", d.prefix));

    const sel = document.createElement("select");
    sel.className = "domain-vault";
    sel.setAttribute("aria-label", `${d.prefix} 의 볼트`);
    const current = vaultOfDomain(d.vault, DEFAULT_VAULT);
    for (const n of names) {
      const opt = document.createElement("option");
      opt.value = n;
      opt.textContent = n === DEFAULT_VAULT ? `${n} (기본)` : n;
      opt.selected = n === current;
      sel.append(opt);
    }
    // **설정에 없는 볼트로 엮여 있으면 그것도 보여야 한다.** 목록에서 조용히
    // 빠지면 화면은 멀쩡해 보이는데 그 프로젝트의 기록은 갈 곳이 없다.
    if (!names.includes(current)) {
      const opt = document.createElement("option");
      opt.value = current;
      opt.textContent = `${current} (⚠️ 없는 볼트)`;
      opt.selected = true;
      sel.prepend(opt);
    }
    sel.addEventListener("change", () => {
      sel.disabled = true;
      // 기본 볼트를 고르면 빈 값으로 되돌린다 — 설정에 줄을 남기지 않는다.
      on.bind(d.prefix, sel.value === DEFAULT_VAULT ? "" : sel.value);
    });
    row.append(sel);
    // **폴더가 접두어와 같으면 안 쓴다.** 열일곱 줄이 전부 `common / common` 처럼
    // 같은 낱말을 두 번 보여 주고 있었다 — 정보가 아니라 잡음이다. 다른 때만
    // 보여 주면 그때는 진짜 알아야 할 사실이 된다.
    if (d.folder && d.folder !== d.prefix) {
      row.append(el("span", "domain-folder", d.folder));
    }
    root.append(row);
  }
}

/** remoteField 는 그 볼트가 동기화할 git 주소를 정하는 자리다.
 *
 * # 왜 볼트 줄 안에 있나
 *
 * 리모트는 **볼트마다 다르다** — 개인 볼트는 없어도 되고 회사 볼트는 회사
 * 인프라를 가리켜야 한다. 따로 화면을 두면 "어느 볼트 얘기인가" 가 사라지고,
 * 그 순간 회사 결정을 개인 리모트에 밀어 넣는 사고가 가능해진다.
 *
 * # 빈 값은 고장이 아니다
 *
 * 리모트가 없으면 그 볼트는 이 머신에만 있다. 정상 상태이므로 경고로 그리지
 * 않는다 — 개인 볼트는 그게 기본이다.
 *
 * 저장은 **버튼을 눌러야** 일어난다. 입력할 때마다 부르면 주소를 치는 도중의
 * 반쪽짜리 문자열이 origin 에 박힌다. */
function remoteField(
  v: { name: string; remote: string; exists: boolean },
  on: VaultActions,
): HTMLElement {
  const box = el("div", "vault-remote");
  box.append(el("label", "vault-remote-label", "git 리모트"));

  const input = document.createElement("input");
  input.className = "vault-remote-input";
  input.type = "text";
  input.value = v.remote ?? "";
  input.placeholder = "없음 — 이 머신에만 있다";
  input.spellcheck = false;
  // 자리가 없는 볼트에는 리모트를 못 붙인다. 눌러도 아무 일이 안 나는 것보다
  // 처음부터 못 누르게 하는 편이 낫다 (열기 버튼과 같은 규칙).
  input.disabled = !v.exists;

  const save = document.createElement("button");
  save.className = "btn";
  save.textContent = "저장";
  const sync = () => {
    const next = input.value.trim();
    save.disabled = !v.exists || next === "" || next === (v.remote ?? "");
  };
  input.addEventListener("input", sync);
  save.addEventListener("click", () => on.remote(v.name, input.value.trim()));
  sync();

  box.append(input, save);
  return box;
}

/** addVaultForm 은 볼트를 만드는 자리다.
 *
 * **경로를 묻지 않는다.** 어디에 만들지는 답이 이미 정해져 있는 질문이다 —
 * 지금 볼트 옆이다. 그 규칙은 CLI 에만 살고(`config.NewVaultPath`), 앱은
 * `vault_parent` 로 결과를 미리 보여 주기만 한다.
 *
 * **어디에 생기는지는 반드시 보여 준다.** 경로를 안 물어봤으므로 이 줄이 없으면
 * 사람은 어디에 만들어졌는지 모르는 폴더를 갖게 된다. 그건 묻는 것보다 나쁘다. */
function addVaultForm(parent: string, on: VaultActions): HTMLElement {
  const box = el("div", "vault-add");
  const name = document.createElement("input");
  name.className = "vault-add-name";
  name.placeholder = "새 볼트 이름 (예: 회사)";
  name.setAttribute("aria-label", "새 볼트 이름");

  const btn = document.createElement("button");
  btn.className = "btn";
  btn.textContent = "만들기";

  const where = el("div", "vault-add-where");
  const sync = (): void => {
    const v = name.value.trim();
    // 이름이 없으면 만들 수 없다. 빈 값으로 부르면 CLI 가 거부하는데,
    // 그 오류를 보게 하느니 못 누르게 하는 편이 낫다.
    btn.disabled = v === "";
    where.textContent = v === "" ? "" : `${parent}/${v}`;
  };
  sync();
  name.addEventListener("input", sync);
  btn.addEventListener("click", () => {
    btn.disabled = true;
    on.add(name.value.trim());
  });

  box.append(name, btn, where);
  return box;
}
