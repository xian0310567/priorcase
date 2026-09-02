#!/usr/bin/env node
// priorcase 런처.
//
// **이 파일은 아무 일도 하지 않는다** — 자기 플랫폼의 네이티브 바이너리를 찾아
// 그대로 넘긴다. esbuild 가 쓰는 방식이고, 이유가 셋이다.
//
//   1. 설치 시 다운로드가 없다. npm 이 optionalDependencies 로 자기 플랫폼 것만
//      받는다 — postinstall 스크립트가 네트워크를 타면 사내망·오프라인에서 죽는다.
//   2. Node 는 **배달부일 뿐이다.** 실행되는 것은 정적 Go 바이너리이고,
//      런타임 의존은 여전히 0 이다 (D1).
//   3. `npx -y priorcase mcp` 가 그대로 된다 — MCP 생태계의 규범이다.
//
// exec 로 프로세스를 갈아탄다. 감싸면 신호(Ctrl-C)와 종료 코드가 한 겹 더 거쳐야
// 하는데, priorcase 는 훅으로 불려서 **종료 코드가 규약**이다.

const { spawnSync } = require("child_process");
const { existsSync } = require("fs");
const path = require("path");

// **npm 의 플랫폼 이름과 Go 의 GOOS 가 다르다.** node 는 win32, Go 는 windows 다
// (그리고 64비트에서도 win32 다 — 역사적 이름이라 안 바뀐다). 패키지 이름은 node 가
// 보는 쪽을 따른다: 이 표를 잘못 적으면 윈도우 사용자가 "no binary" 로 튕긴다.
const PKG = { darwin: "darwin", linux: "linux", win32: "win32" };
const ARCH = { arm64: "arm64", x64: "x64" };

function binaryPath() {
  const os = PKG[process.platform];
  const arch = ARCH[process.arch];
  if (!os || !arch) return null;
  // **스코프를 안 쓴다.** 스코프를 쓰려면 npm 조직이 있어야 하는데, 조직 이름은
  // 미리 확인할 API 가 없어 제출해 봐야만 쓸 수 있는지 안다 (옛 이름 casebook 이
  // 실제로 그렇게 막혔다). 사용자가 치는 명령(npx -y priorcase mcp)에는 스코프가
  // 나오지 않으므로 얻는 것도 없다.
  const pkg = `priorcase-${os}-${arch}`;
  try {
    // require.resolve 가 node_modules 해석을 대신한다 — 경로를 직접 짜면
    // pnpm·yarn PnP 같은 배치에서 깨진다.
    const entry = require.resolve(`${pkg}/package.json`);
    // 윈도우 실행파일에는 확장자가 붙는다. 없으면 spawnSync 가 못 찾는다.
    const exe = process.platform === "win32" ? "prior.exe" : "prior";
    const p = path.join(path.dirname(entry), "bin", exe);
    return existsSync(p) ? p : null;
  } catch {
    return null;
  }
}

// **지원 목록을 손으로 적지 않는다.** 이 목록은 `optionalDependencies` 와 같아야
// 하는데, 손으로 적으면 갈린다 — 실제로 v0.5.0 에서 win32 를 게시하지 못한 채
// 목록에는 남아 있어서, 윈도우 사용자에게 "지원한다, `--include=optional` 로 다시
// 받아라" 라고 **없는 패키지를 가리키는** 안내가 나갈 뻔했다.
//
// 포장 스크립트가 실제로 게시한 것만 optionalDependencies 에 넣으므로(npm-pack.sh 의 §),
// 여기서 그것을 읽으면 안내가 언제나 사실이다.
function supportedTargets() {
  try {
    const deps = require("../package.json").optionalDependencies || {};
    return Object.keys(deps)
      .map((n) => n.replace(/^priorcase-/, ""))
      .sort();
  } catch {
    return [];
  }
}

const bin = binaryPath();
if (!bin) {
  const target = `${process.platform}-${process.arch}`;
  const supported = supportedTargets();
  process.stderr.write(
    `priorcase: no binary for ${target}.\n` +
      (supported.length ? `  Supported: ${supported.join(", ")}.\n` : "") +
      `  If your platform is listed, the optional dependency did not install —\n` +
      `  try: npm install --include=optional priorcase\n`
  );
  // 훅은 언제나 exit 0 이어야 하지만, 여기는 **설치가 안 된 상태**다.
  // 그건 조용히 넘어갈 일이 아니다 — prior doctor 가 볼 수 있도록 실패로 낸다.
  process.exit(1);
}

const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (r.error) {
  process.stderr.write(`priorcase: ${r.error.message}\n`);
  process.exit(1);
}
// 신호로 죽었으면 그 신호를 그대로 흉내 낸다 — 상위 셸이 Ctrl-C 를 알아야 한다.
if (r.signal) {
  process.kill(process.pid, r.signal);
}
process.exit(r.status === null ? 1 : r.status);
