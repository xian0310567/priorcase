import { describe, it, expect, beforeEach } from "vitest";
import { renderHealth } from "../src/render/health";
import type { Health } from "../src/types";

let root: HTMLElement;
beforeEach(() => {
  root = document.createElement("div");
});

describe("renderHealth", () => {
  // ★★ 큐가 셋 다 비었을 때 앱이 보여 줄 것이 이것뿐이다.
  //    비어 있어도 반드시 채운다.
  it("검사가 없으면 그 사실을 말한다", () => {
    renderHealth(root, []);
    expect(root.textContent).toContain("상태 검사를 받지 못했다");
  });

  it("등급별로 다른 표시를 붙인다", () => {
    const items: Health[] = [
      { name: "볼트 personal", level: "ok", detail: "/a" },
      { name: "팀 이식성", level: "warn", detail: "paths 로만 잡힌다", fix: "repos 를 더하라" },
      { name: "볼트 work", level: "fail", detail: "접근할 수 없다" },
    ];
    renderHealth(root, items);
    const t = root.textContent!;
    expect(t).toContain("볼트 personal");
    expect(t).toContain("볼트 work");
    expect(t).toContain("repos 를 더하라");
    const rows = root.querySelectorAll(".health-row");
    expect(rows[0].className).toContain("level-ok");
    expect(rows[1].className).toContain("level-warn");
    expect(rows[2].className).toContain("level-fail");
  });

  // ★ 모르는 등급을 정상으로 뭉개면 하필 알아야 할 상태가 안 보인다.
  it("모르는 등급을 정상으로 그리지 않는다", () => {
    renderHealth(root, [{ name: "새검사", level: "unknown", detail: "x" }]);
    const row = root.querySelector(".health-row")!;
    expect(row.className).not.toContain("level-ok");
    expect(row.className).toContain("level-unknown");
  });

  // ★★ 계약 밖의 값이 와도 정상으로 뭉개지 않는다. TS 타입은 런타임 JSON 을
  //    검사하지 않으므로, Go 쪽에 등급이 하나 늘면 그대로 흘러 들어온다.
  it("계약에 없는 등급도 정상으로 뭉개지 않는다", () => {
    renderHealth(root, [{ name: "새검사", level: "critical" as never, detail: "x" }]);
    const row = root.querySelector(".health-row")!;
    expect(row.className).not.toContain("level-ok");
    expect(row.className).toContain("level-unknown");
  });

  // ★★ 30초마다 다시 그린다.
  it("다시 그려도 쌓이지 않는다", () => {
    for (let i = 0; i < 4; i++) {
      renderHealth(root, [{ name: "볼트", level: "ok", detail: "/a" }]);
    }
    expect(root.querySelectorAll(".health-row").length).toBe(1);
  });

  it("fix 가 없으면 그 줄을 안 만든다", () => {
    renderHealth(root, [{ name: "볼트", level: "ok", detail: "/a" }]);
    expect(root.querySelector(".health-fix")).toBeNull();
  });

  // ★ 등급이 ok 여도 fix 가 오는 판이 있다 (실측: "도메인 폴더" 는 ok 인데
  //   fix 를 단다). 등급으로 fix 를 감추면 그 안내가 사라진다.
  it("ok 여도 fix 가 있으면 보여 준다", () => {
    renderHealth(root, [
      { name: "도메인 폴더", level: "ok", detail: "8개 중 3개는 아직 없다", fix: "folder 값을 확인하라" },
    ]);
    expect(root.textContent).toContain("folder 값을 확인하라");
  });

  // ★★ detail 에는 설정 파일 내용과 경로가 들어온다 — 남의 글이다.
  it("detail 의 마크업이 그대로 글자로 나온다", () => {
    renderHealth(root, [{ name: "x", level: "fail", detail: "<b>깨졌다</b>" }]);
    expect(root.querySelector("b")).toBeNull();
    expect(root.textContent).toContain("<b>깨졌다</b>");
  });
});
