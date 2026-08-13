import { describe, it, expect, beforeEach } from "vitest";
import { renderError, renderWarnings, renderEmpty } from "../src/render/shell";

let el: HTMLElement;
beforeEach(() => {
  el = document.createElement("div");
});

describe("renderError", () => {
  // ★★ 오류 종류마다 사람이 할 일이 다르다. 한 화면으로 뭉치면 안 된다.
  it("설치가 안 됐으면 설치 안내를 낸다", () => {
    renderError(el, { kind: "not_found", message: "prior 를 찾을 수 없다" });
    expect(el.textContent).toContain("설치");
    expect(el.textContent).not.toContain("할 일이 없");
  });

  it("실패는 stderr 를 그대로 보여 준다", () => {
    renderError(el, { kind: "failed", message: "설정을 읽을 수 없다 (/x/config.toml)" });
    expect(el.textContent).toContain("/x/config.toml");
  });

  it("타임아웃은 느리다고 말한다", () => {
    renderError(el, { kind: "timeout", message: "상한 안에 끝나지 않았다" });
    expect(el.textContent).toContain("오래");
  });

  // ★ io 도 계약의 값 넷 중 하나다. 화면이 없으면 빈 상자가 뜬다.
  it("io 도 단서를 보여 준다", () => {
    renderError(el, { kind: "io", message: "파이프가 끊겼다" });
    expect(el.textContent).toContain("파이프가 끊겼다");
  });

  // ★★ **30초마다 다시 그린다.** replaceChildren 을 빠뜨리면 오류가 이어지는
  //    동안 같은 상자가 계속 쌓이고, 창이 끝없이 길어진다.
  it("다시 그려도 쌓이지 않는다", () => {
    for (let i = 0; i < 5; i++) {
      renderError(el, { kind: "timeout", message: "느리다" });
    }
    expect(el.querySelectorAll(".error").length).toBe(1);
  });

  // ★★ **stderr 는 남의 글이다.** prior 의 stderr 에는 설정 파일 내용이나 노트
  //    경로가 섞여 나오고, 그 안에 마크업이 있을 수 있다. textContent 대신
  //    innerHTML 을 쓰면 화면 구조가 통째로 뒤틀린다.
  it("stderr 의 마크업이 그대로 글자로 나온다", () => {
    renderError(el, { kind: "failed", message: "<img src=x onerror=alert(1)>" });
    expect(el.querySelector("img")).toBeNull();
    expect(el.textContent).toContain("<img src=x onerror=alert(1)>");
  });
});

describe("renderWarnings", () => {
  // ★★ warnings 가 있으면 큐가 **불완전**하다. "할 일 없음" 으로 그리면 안 된다.
  it("경고가 있으면 배너를 낸다", () => {
    renderWarnings(el, ["볼트 work 에 접근할 수 없다"]);
    expect(el.textContent).toContain("볼트 work");
    expect(el.textContent).toContain("불완전");
  });

  it("경고가 없으면 아무것도 안 그린다", () => {
    renderWarnings(el, undefined);
    expect(el.innerHTML).toBe("");
    renderWarnings(el, []);
    expect(el.innerHTML).toBe("");
  });

  // ★★ **경고가 해소되면 배너가 사라져야 한다.**
  //
  // 볼트가 잠깐 안 붙었다가 붙는 판이 흔하다(외장 디스크·동기화). 배너가 안
  // 지워지면 사람은 영영 "불완전한 큐" 를 보면서도 실제로는 멀쩡한 큐를 읽고,
  // 그 다음부터 배너를 무시하는 법을 배운다.
  it("경고가 해소되면 배너를 지운다", () => {
    renderWarnings(el, ["볼트 work 에 접근할 수 없다"]);
    expect(el.innerHTML).not.toBe("");
    renderWarnings(el, undefined);
    expect(el.innerHTML).toBe("");
  });
});

describe("renderEmpty", () => {
  // ★ 빈 상태가 "고장난 것처럼" 보이면 안 된다.
  it("무엇이 비었는지 말한다", () => {
    renderEmpty(el, "확인할 구간");
    expect(el.textContent).toContain("확인할 구간");
    expect(el.textContent).toContain("없");
  });
});
